#!/bin/bash
set -exv

BASE_IMG="syslog-cloudwatch-bridge"

IMG="${BASE_IMG}:latest"

BUILD_CMD="docker build" IMG="$IMG" make docker-build
