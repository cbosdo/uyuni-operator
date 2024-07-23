#!/bin/sh

set -xe

# Only add repos when building outside of OBS.
# To use it define the KUBECTL_REPO variable.
if test -n "$1"; then
    zypper -n ar $1 "kubectl repo"
    zypper -n --gpg-auto-import-keys ref
fi
