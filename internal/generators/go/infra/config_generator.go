package infra

import (
	"fmt"
	"strings"
	"unicode"

	"stencil/internal/emitter"
	"stencil/internal/generator"
	"stencil/internal/template"
)

// ── Template data types ─────────────────────────────────────────────────────

// ConfigData is the top-level payload passed to go/infra/config.go.tmpl.
type ConfigData struct {
	Module       string
	ConfigLoader string // "env" | "viper-yaml"
	Vars         []ConfigVar
}

// ConfigVar represents one config variable for the template.
type ConfigVar struct {
	GoName  string // "DatabaseURL"
	GoType  string // "string"
	EnvName string // "DATABASE_URL"
}

// ── Generator ───────────────────────────────────────────────────────────────

type configGenerator struct {
	engine *template.Engine
}

func NewConfigGenerator(e *template.Engine) generator.Generator {
	return &configGenerator{engine: e}
}

func (g *configGenerator) ID() string { return "go.config" }

func (g *configGenerator) Generate(ctx generator.GeneratorContext) ([]emitter.File, error) {
	var vars []ConfigVar
	for _, cv := range ctx.Spec.Config {
		vars = append(vars, ConfigVar{
			GoName:  configToPascalCase(cv.Name),
			GoType:  ctx.Lang.ConfigVarType(cv.YAMLType),
			EnvName: configToEnvName(cv.Name),
		})
	}

	data := ConfigData{
		Module:       ctx.Spec.Module,
		ConfigLoader: ctx.Spec.ConfigLoader,
		Vars:         vars,
	}
	content, err := g.engine.Render("go/infra/config.go.tmpl", data)
	if err != nil {
		return nil, fmt.Errorf("config generator render failed: %w", err)
	}
	return []emitter.File{{Path: "config/config.go", Content: content}}, nil
}

// configToPascalCase converts a name like "database_url" or "DATABASE_URL" to "DatabaseUrl".
func configToPascalCase(s string) string {
	parts := strings.FieldsFunc(s, func(r rune) bool {
		return r == '_' || r == '-'
	})
	var result strings.Builder
	for _, part := range parts {
		if len(part) == 0 {
			continue
		}
		runes := []rune(strings.ToLower(part))
		runes[0] = unicode.ToUpper(runes[0])
		result.WriteString(string(runes))
	}
	return result.String()
}

// configToEnvName converts a name to UPPER_SNAKE_CASE for environment variables.
func configToEnvName(s string) string {
	var result []rune
	for i, r := range s {
		if unicode.IsUpper(r) && i > 0 {
			prev := rune(s[i-1])
			if unicode.IsLower(prev) || unicode.IsDigit(prev) {
				result = append(result, '_')
			}
		}
		result = append(result, unicode.ToUpper(r))
	}
	return string(result)
}
