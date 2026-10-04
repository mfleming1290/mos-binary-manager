#!/bin/sh
# Harmless foreground fixture for an explicit MOS smoke test.
trap 'echo "Loop fixture stopped"; exit 0' TERM INT
while :; do
  printf 'Loop fixture alive; PID=%s\n' "$$"
  sleep 2
done
