#!/bin/sh

set -e

/usr/local/bin/cli seed -size 1M
pm2 start /app/pm2.json
