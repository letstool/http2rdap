#!/bin/bash

IMAGE_TAG=letstool/http2rdap:latest

docker build \
        -t "$IMAGE_TAG" \
       -f build/Dockerfile \
       .
