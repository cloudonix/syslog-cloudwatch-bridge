#!/bin/bash

# AppSRE team CD

set -exv

CURRENT_DIR=$(dirname $0)

BASE_IMG="syslog-cloudwatch-bridge"
QUAY_IMAGE="quay.io/app-sre/${BASE_IMG}"
IMG="${BASE_IMG}:latest"

GIT_HASH=`git rev-parse --short=7 HEAD`

# build the image
BUILD_CMD="docker build" IMG="$IMG" make docker-build

# push the image
skopeo copy --dest-creds "${QUAY_USER}:${QUAY_TOKEN}" \
    "docker-daemon:${IMG}" \
    "docker://${QUAY_IMAGE}:latest"

skopeo copy --dest-creds "${QUAY_USER}:${QUAY_TOKEN}" \
    "docker-daemon:${IMG}" \
    "docker://${QUAY_IMAGE}:${GIT_HASH}"


export DOCKER_CONF="$PWD/.docker"
mkdir -p "${DOCKER_CONF}"

# login to the backup repository
aws ecr get-login \
    --region ${AWS_REGION} --no-include-email | \
    sed 's/docker/docker --config="$DOCKER_CONF"/g' | \
    /bin/bash

# push the image
skopeo copy \
    --authfile "$DOCKER_CONF/config.json" \
    "docker-daemon:${IMG}" \
    "docker://${BACKUP_REPO_URL}:latest"

skopeo copy \
    --authfile "$DOCKER_CONF/config.json" \
    "docker-daemon:${IMG}" \
    "docker://${BACKUP_REPO_URL}:${GIT_HASH}"
