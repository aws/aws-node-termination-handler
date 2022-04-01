#!/usr/bin/env bash

aws ecr get-login-password \
    --region "${AWS_REGION:?AWS_REGION is undefined or empty}" | \
    docker login \
        --username AWS \
        --password-stdin \
        "${KO_DOCKER_REPO:?KO_DOCKER_REPO is undefined or empty}"
