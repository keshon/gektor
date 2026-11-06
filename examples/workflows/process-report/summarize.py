import json
import sys

services = json.load(sys.stdin)["services"]
print("# Service report\n")
print(f"{len(services)} services checked.\n")
for service in services:
    print(f"- {service['name']}: {service['status']}")
