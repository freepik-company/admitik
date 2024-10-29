#!/bin/bash

# Execute the command in the background and save the logs
nohup ssh -R 80:localhost:10250 nokey@localhost.run > /tmp/admitik_tunnel_logs.txt 2>&1 &

# Wait until the logs file has content
while [ ! -s /tmp/admitik_tunnel_logs.txt ]; do
  sleep 1
done

# Wait until the line with the URL appears in the logs
while true; do
  HOSTNAME=$(grep 'tunneled with tls termination' /tmp/admitik_tunnel_logs.txt | awk '{print $1}')
  if [ ! -z "$HOSTNAME" ]; then
    echo "$HOSTNAME"
    break
  else
    sleep 1
  fi
done