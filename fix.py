import re, glob

# 1. Fix parser.go
with open('internal/spec/parser/parser.go', 'r') as f:
    c = f.read()
c = c.replace('NewParser()()', 'NewParser()')
c = c.replace('import "stencil/internal/spec"\n\n\nimport (', 'import (\n\t"stencil/internal/spec"\n')
with open('internal/spec/parser/parser.go', 'w') as f:
    f.write(c)

# 2. Fix validator_test.go
with open('internal/spec/validator/validator_test.go', 'r') as f:
    c = f.read()
c = c.replace('NewParser()()', 'parser.NewParser()')
c = c.replace('import "stencil/internal/spec"\n\nimport (', 'import (\n\t"stencil/internal/spec"\n\t"stencil/internal/spec/parser"\n')
with open('internal/spec/validator/validator_test.go', 'w') as f:
    f.write(c)

# 3. Fix resolver files
for path in glob.glob('internal/spec/resolver/*.go'):
    with open(path, 'r') as f: c = f.read()
    c = re.sub(r'(?<!spec\.)Resolved', 'spec.Resolved', c)
    c = re.sub(r'(?<!spec\.)ContextField', 'spec.ContextField', c)
    c = re.sub(r'(?<!spec\.)QueryParam', 'spec.QueryParam', c)
    c = re.sub(r'(?<!spec\.)TableAST', 'spec.TableAST', c)
    c = re.sub(r'(?<!spec\.)FieldAST', 'spec.FieldAST', c)
    c = re.sub(r'(?<!spec\.)ExternalAST', 'spec.ExternalAST', c)
    c = re.sub(r'(?<!spec\.)ExternalCallAST', 'spec.ExternalCallAST', c)
    c = re.sub(r'(?<!spec\.)TransactionAST', 'spec.TransactionAST', c)
    c = re.sub(r'(?<!spec\.)ResourceGroupAST', 'spec.ResourceGroupAST', c)
    c = re.sub(r'(?<!spec\.)APIAST', 'spec.APIAST', c)
    c = re.sub(r'(?<!spec\.)TouchAST', 'spec.TouchAST', c)
    c = re.sub(r'(?<!spec\.)SpecAST', 'spec.SpecAST', c)
    # Fix any accidental spec.spec.
    c = c.replace('spec.spec.', 'spec.')
    with open(path, 'w') as f: f.write(c)
