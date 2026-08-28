# Security policy

Only the latest release and `main` branch are supported with security fixes.

Report vulnerabilities through GitHub private vulnerability reporting. Do not
open a public issue containing credentials, exploit details, Docker access, or
player data. Include the affected version, reproduction steps, and impact. You
should receive an acknowledgement within seven days.

The public web process must never mount the Docker socket. Docker authority is
isolated in the private `docker-broker` service, which accepts only authenticated
operations for exact names in `BROKER_ALLOWED_CONTAINERS`.
