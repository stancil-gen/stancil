package container

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
)

type Container struct {
	mu         sync.RWMutex
	providers  map[reflect.Type]reflect.Value
	cache      map[reflect.Type]reflect.Value
	startHooks []func(context.Context) error
	stopHooks  []func(context.Context) error
}

func New() *Container {
	return &Container{
		providers: map[reflect.Type]reflect.Value{},
		cache:     map[reflect.Type]reflect.Value{},
	}
}

// Provide registers a constructor function.
func (c *Container) Provide(constructor interface{}) {
	fn := reflect.TypeOf(constructor)
	if fn.Kind() != reflect.Func {
		panic(fmt.Sprintf("container.Provide: expected func, got %T", constructor))
	}
	if fn.NumOut() == 0 {
		panic("container.Provide: constructor must return at least one value")
	}
	outType := fn.Out(0)
	c.mu.Lock()
	c.providers[outType] = reflect.ValueOf(constructor)
	c.mu.Unlock()
}

// MustResolve builds the target type and all its transitive dependencies.
func (c *Container) MustResolve(target interface{}) {
	t := reflect.TypeOf(target)
	if t.Kind() != reflect.Ptr {
		panic("container.MustResolve: target must be a pointer")
	}
	val := c.resolveWithStack(t.Elem(), []reflect.Type{})
	reflect.ValueOf(target).Elem().Set(val)
}

func (c *Container) resolveWithStack(t reflect.Type, stack []reflect.Type) reflect.Value {
	// Cycle detection
	for _, s := range stack {
		if s == t {
			chain := make([]string, len(stack)+1)
			for i, stackType := range stack {
				chain[i] = stackType.String()
			}
			chain[len(stack)] = t.String()
			panic(fmt.Sprintf("container: circular dependency detected: %s", strings.Join(chain, " -> ")))
		}
	}

	c.mu.RLock()
	if cached, ok := c.cache[t]; ok {
		c.mu.RUnlock()
		return cached
	}
	c.mu.RUnlock()

	c.mu.RLock()
	constructor, ok := c.providers[t]
	c.mu.RUnlock()
	if !ok {
		panic(fmt.Sprintf("container: no provider registered for %v", t))
	}

	fnType := constructor.Type()
	args := make([]reflect.Value, fnType.NumIn())
	newStack := append(stack, t)

	for i := 0; i < fnType.NumIn(); i++ {
		args[i] = c.resolveWithStack(fnType.In(i), newStack)
	}

	results := constructor.Call(args)
	val := results[0]

	c.mu.Lock()
	c.cache[t] = val
	c.mu.Unlock()

	return val
}

func (c *Container) OnStart(fn func(context.Context) error) {
	c.startHooks = append(c.startHooks, fn)
}

func (c *Container) OnStop(fn func(context.Context) error) {
	c.stopHooks = append(c.stopHooks, fn)
}

func (c *Container) Start(ctx context.Context) error {
	for _, fn := range c.startHooks {
		if err := fn(ctx); err != nil {
			return err
		}
	}
	<-ctx.Done()
	return nil
}

func (c *Container) Stop(ctx context.Context) error {
	var errs []error
	for i := len(c.stopHooks)-1; i >= 0; i-- {
		if err := c.stopHooks[i](ctx); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}
