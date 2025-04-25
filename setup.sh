#!/bin/bash

set -e  # Exit immediately if a command exits with a non-zero status

# Define variables
SERVER_IP="172.20.163.201"  # Replace with your server's actual IP address
SERVICE_NAME="seuic_reader"
SYSTEMD_FILE="/etc/systemd/system/${SERVICE_NAME}.service"
NGINX_CONF="/etc/nginx/sites-enabled/seuic_websocket"

# Check if the service already exists
if systemctl list-units --full -all | grep -Fq "${SERVICE_NAME}.service"; then
    echo "Service '${SERVICE_NAME}' already exists. Skipping service setup."
else
    # Building Go binary
    echo "Building Go binary..."
    go build -o seuic_reader main.go

    # Moving binary to /usr/local/bin/
    echo "Moving binary to /usr/local/bin/..."
    sudo mv seuic_reader /usr/local/bin/

    # Creating user and group
    echo "Creating user and group..."
    sudo groupadd -f seuicuser_group
    if ! id -u seuic_user >/dev/null 2>&1; then
        sudo useradd -r -s /bin/false -g seuicuser_group seuic_user
    fi

    # Setting permissions
    echo "Setting permissions for binary..."
    sudo chown seuic_user:seuicuser_group /usr/local/bin/seuic_reader

    # Creating systemd service file
    echo "Creating systemd service file..."
    sudo bash -c "cat > $SYSTEMD_FILE" <<EOF
[Service]
ExecStart=/usr/local/bin/seuic_reader
Restart=always
RestartSec=5
User=seuic_user
Group=seuicuser_group
KillSignal=SIGTERM
EOF

    # Reload systemctl daemon and enable the service
    echo "Reloading systemd and enabling service..."
    sudo systemctl daemon-reload
    sudo systemctl enable ${SERVICE_NAME}.service
    sudo systemctl start ${SERVICE_NAME}.service
fi

# Generating SSL Certificates if not already created
if [[ ! -f /etc/nginx/ssl/server.key ]] || [[ ! -f /etc/nginx/ssl/server.crt ]]; then
    echo "Generating SSL certificates for NGINX..."
    sudo mkdir -p /etc/nginx/ssl
    openssl req -x509 -nodes -days 365 -newkey rsa:2048 \
        -keyout /etc/nginx/ssl/server.key \
        -out /etc/nginx/ssl/server.crt \
        -subj "/CN=${SERVER_IP}"

    # Copy certificate for system-wide sharing
    echo "Updating system-wide CA certificates..."
    sudo cp /etc/nginx/ssl/server.crt /usr/local/share/ca-certificates/
    sudo update-ca-certificates
else
    echo "SSL certificates already exist. Skipping SSL generation."
fi

# Generate PEM certificates if not created
if [[ ! -f /etc/nginx/ssl/privkey.pem ]] || [[ ! -f /etc/nginx/ssl/fullchain.pem ]]; then
    echo "Generating PEM certificates for NGINX..."
    openssl req -x509 -nodes -days 365 -newkey rsa:2048 \
        -keyout privkey.pem -out fullchain.pem \
        -subj "/CN=${SERVER_IP}"

    # Move PEM files to proper location and set permissions
    echo "Configuring PEM certificates..."
    sudo mkdir -p /etc/nginx/ssl
    sudo mv privkey.pem /etc/nginx/ssl/
    sudo mv fullchain.pem /etc/nginx/ssl/
    sudo chmod 700 /etc/nginx/ssl
    sudo chmod 600 /etc/nginx/ssl/*
    sudo chown -R root:root /etc/nginx/ssl
else
    echo "PEM certificates already exist. Skipping PEM generation."
fi

# Configure NGINX if the config file doesn't exist
if [[ ! -f $NGINX_CONF ]]; then
    echo "Configuring NGINX..."
    sudo bash -c "cat > $NGINX_CONF" <<EOF
server {
    listen 443 ssl;
    server_name ${SERVER_IP};

    ssl_certificate     /etc/nginx/ssl/fullchain.pem;
    ssl_certificate_key /etc/nginx/ssl/privkey.pem;

    location / {
        proxy_pass http://localhost:8765/;
        proxy_http_version 1.1;
        proxy_set_header Upgrade \$http_upgrade;
        proxy_set_header Connection "upgrade";
        proxy_set_header Host \$host;
    }
}
EOF
    # Testing and reloading NGINX
    echo "Testing NGINX configuration..."
    sudo nginx -t
    echo "Reloading NGINX..."
    sudo systemctl reload nginx
    sudo systemctl restart nginx
else
    echo "NGINX configuration already exists. Skipping NGINX configuration."
fi

# Tailing NGINX logs
echo "Tailing NGINX logs (exit with Ctrl+C)..."
sudo tail -f /var/log/nginx/error.log /var/log/nginx/access.log