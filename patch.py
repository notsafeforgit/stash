import re

with open('pkg/sqlite/migrations/85_postmigrate.go', 'r') as file:
    content = file.read()

content = content.replace('''				if depth != nil {
					criterion["depth"] = depth
				}
			}
		} else {
			// Primitive values, no action needed
		}
	}''', '''				if depth != nil {
					criterion["depth"] = depth
				}
			}
		}
	}''')

with open('pkg/sqlite/migrations/85_postmigrate.go', 'w') as file:
    file.write(content)
