import re

with open('pkg/sqlite/migrations/85_postmigrate.go', 'r') as file:
    content = file.read()

content = content.replace("} else if _, isArray := v.([]interface{}); !isArray {", "} else {")

with open('pkg/sqlite/migrations/85_postmigrate.go', 'w') as file:
    file.write(content)
