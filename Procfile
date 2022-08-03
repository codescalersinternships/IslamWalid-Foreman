service_ping:
  cmd: ping -c 5 google.com | grep google
  checks:
    cmd: ls
  deps: 
      - service_redis

service_sleep:
  cmd: sleep infinity
  checks:
    cmd: ls
  deps: 
      - service_ping

service_redis:
  cmd: redis-server --port 6010
  run_once: true
  checks:
    cmd: redis-cli -p 6010 ping
