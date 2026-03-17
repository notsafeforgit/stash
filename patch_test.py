import re

with open('pkg/sqlite/migrations/85_postmigrate_test.go', 'r') as file:
    content = file.read()

content = content.replace('''	var outMap, expMap map[string]interface{}
	json.Unmarshal(output, &outMap)
	json.Unmarshal([]byte(expected), &expMap)

	outJSON, _ := json.Marshal(outMap)
	expJSON, _ := json.Marshal(expMap)''', '''	var outMap, expMap map[string]interface{}
	if err := json.Unmarshal(output, &outMap); err != nil {
		t.Fatalf("unexpected unmarshal error: %v", err)
	}
	if err := json.Unmarshal([]byte(expected), &expMap); err != nil {
		t.Fatalf("unexpected unmarshal error: %v", err)
	}

	outJSON, err := json.Marshal(outMap)
	if err != nil {
		t.Fatalf("unexpected marshal error: %v", err)
	}
	expJSON, err := json.Marshal(expMap)
	if err != nil {
		t.Fatalf("unexpected marshal error: %v", err)
	}''')

with open('pkg/sqlite/migrations/85_postmigrate_test.go', 'w') as file:
    file.write(content)
