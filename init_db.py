import requests

url = 'http://localhost:9999/graphql'
query = """
mutation Setup($input: SetupInput!) {
    setup(input: $input)
}
"""
variables = {
    "input": {
        "configLocation": "/home/jules/.stash/config.yml",
        "stashes": [{"path": "/home/jules/stash-data", "excludeVideo": False, "excludeImage": False}],
        "databaseFile": "/home/jules/.stash/stash-go.sqlite",
        "generatedLocation": "/home/jules/.stash/generated",
        "cacheLocation": "/home/jules/.stash/cache",
        "storeBlobsInDatabase": True,
        "blobsLocation": "/home/jules/.stash/blobs"
    }
}
r = requests.post(url, json={'query': query, 'variables': variables})
print("Init response:", r.json())
