#!/usr/bin/env bash

usage=$(cat << EOM
usage: $(basename "$0") -h | STACK_NAME OUTPUT_NAME

    Get the specified output value from a Cloud Formation stack.

    Options:
        -h      Print usage message then exit.

    Arguments:
        STACK_NAME   Name of Cloud Formation stack.
        OUTPUT_NAME  Name of output in Cloud Formation stack.

EOM
)

while getopts "h" opt; do
    case $opt in
        h ) echo "${usage}"
            exit 0
            ;;
        \? ) echo "${usage}" 1>&2
             exit 1
             ;;
    esac
done

stack_name="$1"

if [[ -z "${stack_name}" ]]; then
    echo "error: missing stack name" 1>&2
    echo 1>&2
    echo "${usage}" 1>&2
    exit 1
fi

output_name="$2"

if [[ -z "${output_name}" ]]; then
    echo "error: missing output name" 1>&2
    echo 1>&2
    echo "${usage}" 1>&2
    exit 1
fi

aws cloudformation describe-stacks \
    --stack-name "${stack_name}" \
    --query "Stacks[0].Outputs[?OutputKey=='${output_name}'].OutputValue | [0]" \
    --output text
