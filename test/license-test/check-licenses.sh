#!/bin/bash
set -euo pipefail

ROOT_PATH="$( cd "$(dirname "$0")" ; pwd -P )/../../"
EXIT_CODE=0

while read pkg_license_tuple; do
    pkg=$(echo $pkg_license_tuple | tr -s " " | cut -d" " -f1)
    license=$(echo $pkg_license_tuple | tr -s " " | cut -d" " -f2-)
    if [[ "$(grep -c ${pkg} ${ROOT_PATH}/THIRD_PARTY_LICENSES)" -ge 1 ]]; then
        echo "✅ FOUND ${pkg} (${license})"
    else
        echo "🔴 MISSING for ${pkg} (${license})"
        EXIT_CODE=1
    fi
done < "${1:-/dev/stdin}"

echo "================================================="
if [[ "${EXIT_CODE}" -eq 0 ]]; then
    echo "✅ TEST PASSED"
else
    echo "🔴 TEST FAILED"
fi

exit $EXIT_CODE
