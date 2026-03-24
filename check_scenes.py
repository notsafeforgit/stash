import requests

url = 'http://localhost:9999/graphql'
query = """
query {
    findScenes(scene_filter: {details: {value: "foo", modifier: EQUALS}}) {
        count
    }
}
"""
r = requests.post(url, json={'query': query})
print(r.json())
