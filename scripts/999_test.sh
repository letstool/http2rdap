#!/bin/bash

echo "=== Domain lookup ==="
curl -s -X POST http://127.0.0.1:8080/api/v1/rdap \
     -H "Content-Type: application/json" \
     -d '{"domain":"example.com","timeout":10}' | jq

echo ""
echo "=== IPv4 lookup ==="
curl -s -X POST http://127.0.0.1:8080/api/v1/rdap \
     -H "Content-Type: application/json" \
     -d '{"ip":"8.8.8.8"}' | jq

echo ""
echo "=== ASN lookup ==="
curl -s -X POST http://127.0.0.1:8080/api/v1/rdap \
     -H "Content-Type: application/json" \
     -d '{"asn":"15169"}' | jq
