FROM grafana/grafana:latest

# Set environment variables
ENV GF_SECURITY_ADMIN_USER=admin
ENV GF_SECURITY_ADMIN_PASSWORD=admin
ENV GF_AUTH_ANONYMOUS_ENABLED=true
ENV GF_AUTH_ANONYMOUS_ORG_ROLE=Admin
ENV GF_USERS_ALLOW_SIGN_UP=false

# Copy provisioning files
COPY provisioning/ /etc/grafana/provisioning/

# Set permissions
USER root
RUN chown -R grafana:root /etc/grafana/provisioning/
USER grafana

EXPOSE 3000
