#!/bin/bash
#
# Script to generate self-signed certificates for HTTPS proxy testing
# This generates a CA certificate, server certificate, and client certificate
#

set -e

# Check for OpenSSL
if ! command -v openssl &> /dev/null; then
    echo "Error: OpenSSL is required but not installed."
    exit 1
fi

# Create output directory
OUTPUT_DIR="./certs"
mkdir -p "$OUTPUT_DIR"

# Configuration
CA_CN="HTTPS Proxy Test CA"
SERVER_CN="localhost"
ADMIN_CN="admin.localhost"
CLIENT_CN="client.localhost"
DAYS_VALID=365
KEY_SIZE=2048

echo "Generating certificates for HTTPS Proxy testing..."
echo "Output directory: $OUTPUT_DIR"

# Generate CA key and certificate
echo "Generating CA certificate..."
openssl genrsa -out "$OUTPUT_DIR/ca.key" $KEY_SIZE
openssl req -new -x509 -key "$OUTPUT_DIR/ca.key" -out "$OUTPUT_DIR/ca.pem" \
    -days $DAYS_VALID -subj "/CN=$CA_CN" \
    -addext "basicConstraints=critical,CA:TRUE" \
    -addext "keyUsage=critical,keyCertSign,cRLSign"

# Generate server key and certificate
echo "Generating server certificate..."
openssl genrsa -out "$OUTPUT_DIR/key.pem" $KEY_SIZE
openssl req -new -key "$OUTPUT_DIR/key.pem" -out "$OUTPUT_DIR/server.csr" \
    -subj "/CN=$SERVER_CN"

# Create server certificate extensions file
cat > "$OUTPUT_DIR/server_ext.cnf" << EOF
basicConstraints = CA:FALSE
keyUsage = critical, digitalSignature, keyEncipherment
extendedKeyUsage = serverAuth
subjectAltName = DNS:localhost, IP:127.0.0.1
EOF

# Sign the server certificate
openssl x509 -req -in "$OUTPUT_DIR/server.csr" -CA "$OUTPUT_DIR/ca.pem" \
    -CAkey "$OUTPUT_DIR/ca.key" -CAcreateserial -out "$OUTPUT_DIR/cert.pem" \
    -days $DAYS_VALID -extfile "$OUTPUT_DIR/server_ext.cnf"

# Generate admin key and certificate
echo "Generating admin certificate..."
openssl genrsa -out "$OUTPUT_DIR/admin_key.pem" $KEY_SIZE
openssl req -new -key "$OUTPUT_DIR/admin_key.pem" -out "$OUTPUT_DIR/admin.csr" \
    -subj "/CN=$ADMIN_CN"

# Create admin certificate extensions file
cat > "$OUTPUT_DIR/admin_ext.cnf" << EOF
basicConstraints = CA:FALSE
keyUsage = critical, digitalSignature, keyEncipherment
extendedKeyUsage = serverAuth, clientAuth
subjectAltName = DNS:admin.localhost, IP:127.0.0.1
EOF

# Sign the admin certificate
openssl x509 -req -in "$OUTPUT_DIR/admin.csr" -CA "$OUTPUT_DIR/ca.pem" \
    -CAkey "$OUTPUT_DIR/ca.key" -CAcreateserial -out "$OUTPUT_DIR/admin_cert.pem" \
    -days $DAYS_VALID -extfile "$OUTPUT_DIR/admin_ext.cnf"

# Generate client key and certificate
echo "Generating client certificate..."
openssl genrsa -out "$OUTPUT_DIR/client.key" $KEY_SIZE
openssl req -new -key "$OUTPUT_DIR/client.key" -out "$OUTPUT_DIR/client.csr" \
    -subj "/CN=$CLIENT_CN"

# Create client certificate extensions file
cat > "$OUTPUT_DIR/client_ext.cnf" << EOF
basicConstraints = CA:FALSE
keyUsage = critical, digitalSignature
extendedKeyUsage = clientAuth
EOF

# Sign the client certificate
openssl x509 -req -in "$OUTPUT_DIR/client.csr" -CA "$OUTPUT_DIR/ca.pem" \
    -CAkey "$OUTPUT_DIR/ca.key" -CAcreateserial -out "$OUTPUT_DIR/client.pem" \
    -days $DAYS_VALID -extfile "$OUTPUT_DIR/client_ext.cnf"

# Create PKCS#12 file for client (for browser import)
echo "Creating client PKCS#12 bundle..."
openssl pkcs12 -export -out "$OUTPUT_DIR/client.p12" \
    -inkey "$OUTPUT_DIR/client.key" -in "$OUTPUT_DIR/client.pem" \
    -certfile "$OUTPUT_DIR/ca.pem" \
    -passout pass:changeit

# Create a combined PEM file for curl testing
cat "$OUTPUT_DIR/client.pem" "$OUTPUT_DIR/client.key" > "$OUTPUT_DIR/client_combined.pem"

# Copy CA certificate to trustroot.pem
cp "$OUTPUT_DIR/ca.pem" "$OUTPUT_DIR/trustroot.pem"

# Clean up temporary files
rm "$OUTPUT_DIR/server.csr" "$OUTPUT_DIR/admin.csr" "$OUTPUT_DIR/client.csr" \
   "$OUTPUT_DIR/server_ext.cnf" "$OUTPUT_DIR/admin_ext.cnf" "$OUTPUT_DIR/client_ext.cnf" \
   "$OUTPUT_DIR/ca.srl"

echo ""
echo "Certificate generation complete!"
echo ""
echo "Files generated:"
echo "- CA Certificate:       $OUTPUT_DIR/ca.pem"
echo "- Server Certificate:   $OUTPUT_DIR/cert.pem"
echo "- Server Key:           $OUTPUT_DIR/key.pem"
echo "- Admin Certificate:    $OUTPUT_DIR/admin_cert.pem"
echo "- Admin Key:            $OUTPUT_DIR/admin_key.pem"
echo "- Client Certificate:   $OUTPUT_DIR/client.pem"
echo "- Client Key:           $OUTPUT_DIR/client.key"
echo "- Client PKCS#12:       $OUTPUT_DIR/client.p12 (password: changeit)"
echo "- TrustRoot:            $OUTPUT_DIR/trustroot.pem"
echo ""
echo "To test with curl:"
echo "curl --cert $OUTPUT_DIR/client_combined.pem --cacert $OUTPUT_DIR/ca.pem https://localhost:8443"
echo ""
echo "To install the client certificate in your browser, import $OUTPUT_DIR/client.p12"
echo "with the password 'changeit'"

chmod +x "$0" 