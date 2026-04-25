import os
import re

def rep(c, old, new):
    return c.replace(old, new)

pkg_mappings = {
    'parser': ['parser.go', 'parser_test.go'],
    'validator': ['validator.go', 'validator_test.go'],
    'resolver': ['resolver.go', 'resolve_tables.go', 'resolve_transactions.go', 'resolve_externals.go', 'resolve_apis.go', 'resolve_types.go']
}

for pkg, files in pkg_mappings.items():
    for f in files:
        path = f"internal/spec/{pkg}/{f}"
        if not os.path.exists(path): 
            print(f"File not found: {path}")
            continue
        with open(path, 'r') as file: 
            content = file.read()
        
        # Change package declarations
        content = content.replace("package spec", f"package {pkg}\n\nimport \"stencil/internal/spec\"\n")
        
        # Shared AST mappings
        replacements = [
            ("NewParser()", "NewParser()"),
            ("SpecAST", "spec.SpecAST"),
            ("spec.spec.SpecAST", "spec.SpecAST"), # fix double injection
            ("Resolved", "spec.Resolved"),
            ("spec.spec.Resolved", "spec.Resolved"),
            ("FieldAST", "spec.FieldAST"),
            ("TableAST", "spec.TableAST"),
            ("ResourceGroupAST", "spec.ResourceGroupAST"),
            ("APIAST", "spec.APIAST"),
            ("TouchAST", "spec.TouchAST"),
            ("QueryParam", "spec.QueryParam"),
            ("ContextField", "spec.ContextField"),
        ]
        for old, new in replacements:
            # use regex to avoid replacing mid-word
            content = re.sub(rf'\b{old}\b', new, content)
            
        # Clean up double imports if they exist
        content = content.replace("spec.spec.", "spec.")
        
        with open(path, 'w') as file: 
            file.write(content)

