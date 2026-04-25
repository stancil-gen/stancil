package container

import (
	"context"
	"testing"
)

type Database struct {
	Conn string
}

func NewDatabase() *Database {
	return &Database{Conn: "Connected"}
}

type UserRepository struct {
	DB *Database
}

func NewUserRepository(db *Database) *UserRepository {
	return &UserRepository{DB: db}
}

type UserService struct {
	Repo *UserRepository
}

func NewUserService(repo *UserRepository) *UserService {
	return &UserService{Repo: repo}
}

func TestContainer_ResolveChain(t *testing.T) {
	c := New()
	c.Provide(NewDatabase)
	c.Provide(NewUserRepository)
	c.Provide(NewUserService)

	var svc *UserService
	c.MustResolve(&svc)

	if svc == nil {
		t.Fatalf("Service not resolved")
	}
	if svc.Repo == nil {
		t.Fatalf("Repository not injected")
	}
	if svc.Repo.DB == nil {
		t.Fatalf("Database not injected")
	}
	if svc.Repo.DB.Conn != "Connected" {
		t.Errorf("Database connection state malformed")
	}
}

// Singletons Property Check
func TestContainer_ResolvesSingletons(t *testing.T) {
	c := New()
	c.Provide(NewDatabase)

	var db1 *Database
	var db2 *Database
	c.MustResolve(&db1)
	c.MustResolve(&db2)

	if db1 != db2 {
		t.Errorf("Expected identical instance pointers (Singleton), got distinct instances")
	}
}

// Circular dep tests
type A struct{ B *B }
type B struct{ C *C }
type C struct{ A *A }

func NewA(b *B) *A { return &A{B: b} }
func NewB(c *C) *B { return &B{C: c} }
func NewC(a *A) *C { return &C{A: a} }

func TestContainer_CircularDependencyPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Errorf("Expected circular dependency to panic")
		}
	}()

	c := New()
	c.Provide(NewA)
	c.Provide(NewB)
	c.Provide(NewC)

	var a *A
	c.MustResolve(&a)
}

func TestContainer_Lifecycle(t *testing.T) {
	c := New()
	ctx := context.Background()

	startCount := 0
	c.OnStart(func(ctx context.Context) error {
		startCount++
		return nil
	})

	stopCount := 0
	c.OnStop(func(ctx context.Context) error {
		stopCount++
		return nil
	})

	// simulate isolated run without blocking on ctx.Done()
	ctxCancel, cancel := context.WithCancel(ctx)
	cancel() // Immediately trigger cancel so Start doesn't block forever
	
	_ = c.Start(ctxCancel)
	_ = c.Stop(context.Background())

	if startCount != 1 || stopCount != 1 {
		t.Errorf("Lifecycle hooks did not execute correctly")
	}
}
