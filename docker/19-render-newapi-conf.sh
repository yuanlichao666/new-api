#!/bin/sh
set -eu

: "${NEW_API_BACKEND_HOST:=new-api-backend}"
: "${NEW_API_BACKEND_PORT:=3000}"
export NEW_API_BACKEND_HOST NEW_API_BACKEND_PORT

envsubst '${NEW_API_BACKEND_HOST} ${NEW_API_BACKEND_PORT}' \
  < /etc/nginx/newapi/default.conf.template \
  > /etc/nginx/conf.d/default.conf
