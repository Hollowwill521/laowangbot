#!/bin/sh
# POSIX example; Windows users provide their own executable.
IFS= read -r request || exit 1
printf '%s\n' '{"version":1,"text":"Hello from an independent plugin process!"}'
