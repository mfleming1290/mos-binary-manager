#!/bin/sh
# Intentional nonzero exit: enable only when testing restart backoff.
printf '%s\n' 'Intentional fixture failure (exit 7)' >&2
exit 7
