#!/bin/bash

# Define the SSL folder
SSL_FOLDER="./ssl"

# Check if the SSL folder exists, if not create it
if [ ! -d "$SSL_FOLDER" ]; then
  echo "SSL folder does not exist. Creating $SSL_FOLDER..."
  mkdir -p "$SSL_FOLDER"
fi

# Path to the configuration file
SAN_CONFIG="san.cnf"

# Check if SAN configuration file exists
if [ ! -f "$SAN_CONFIG" ]; then
  echo "Error: SAN configuration file ($SAN_CONFIG) not found!"
  echo "Please ensure $SAN_CONFIG exists in the current directory."
  exit 1
fi

# Generate SSL certificate and private key
echo "Generating SSL certificate and private key in the $SSL_FOLDER folder..."
openssl req -x509 -nodes -days 365 -newkey rsa:2048 \
  -keyout "$SSL_FOLDER/server.key" \
  -out "$SSL_FOLDER/server.crt" \
  -config "$SAN_CONFIG"

# Verify the result
if [ $? -eq 0 ]; then
  echo "SSL certificate and private key successfully generated:"
  echo "  Private Key: $SSL_FOLDER/server.key"
  echo "  Certificate: $SSL_FOLDER/server.crt"
else
  echo "Error: Failed to generate SSL certificate and private key."
  exit 1
fi