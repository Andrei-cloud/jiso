#!/usr/bin/env bash
set -e

# Interactive/configurable script to generate PEM test certificates for mTLS testing in JISO.
# Supports environment variables or interactive prompts for Subject DN attributes.

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
OUTPUT_DIR="${OUTPUT_DIR:-$PROJECT_ROOT/testdata/certs}"

echo "============================================================"
echo "JISO Test Certificate Generator (mTLS PEM)"
echo "============================================================"
echo "Output directory: $OUTPUT_DIR"
echo ""

# Helper to read input with default
prompt_var() {
    local var_name="$1"
    local prompt_text="$2"
    local default_val="$3"
    
    if [ -n "${!var_name}" ]; then
        eval "$var_name=\"${!var_name}\""
    elif [ -t 0 ]; then
        read -p "$prompt_text [$default_val]: " input_val
        if [ -z "$input_val" ]; then
            eval "$var_name=\"$default_val\""
        else
            eval "$var_name=\"$input_val\""
        fi
    else
        eval "$var_name=\"$default_val\""
    fi
}

prompt_var "COUNTRY" "Country Name (2 letter code)" "US"
prompt_var "STATE" "State or Province Name" "California"
prompt_var "LOCALITY" "Locality Name (city)" "San Francisco"
prompt_var "ORG" "Organization Name" "JISO Payment Testing"
prompt_var "OU" "Organizational Unit Name" "Engineering"
prompt_var "CA_CN" "CA Common Name" "JISO Scheme Test Root CA"
prompt_var "SERVER_CN" "Server Common Name" "localhost"
prompt_var "CLIENT_CN" "Client Common Name" "JISO Client SMC"
prompt_var "DAYS" "Validity Period (days)" "365"

mkdir -p "$OUTPUT_DIR"

echo ""
echo "Generating Certificate Authority (Root CA)..."
CA_SUBJ="/C=$COUNTRY/ST=$STATE/L=$LOCALITY/O=$ORG/OU=$OU/CN=$CA_CN"
openssl genrsa -out "$OUTPUT_DIR/ca.key" 4096
openssl req -new -x509 -days "$DAYS" -key "$OUTPUT_DIR/ca.key" -out "$OUTPUT_DIR/ca.crt" -subj "$CA_SUBJ"

echo "Generating Server Private Key and CSR..."
SERVER_SUBJ="/C=$COUNTRY/ST=$STATE/L=$LOCALITY/O=$ORG/OU=$OU/CN=$SERVER_CN"
openssl genrsa -out "$OUTPUT_DIR/server.key" 2048
openssl req -new -key "$OUTPUT_DIR/server.key" -out "$OUTPUT_DIR/server.csr" -subj "$SERVER_SUBJ"

# SAN extension config file for server
SAN_CONFIG="$OUTPUT_DIR/server_ext.cnf"
cat <<EOF > "$SAN_CONFIG"
basicConstraints = CA:FALSE
keyUsage = digitalSignature, keyEncipherment
extendedKeyUsage = serverAuth
subjectAltName = @alt_names

[alt_names]
DNS.1 = localhost
DNS.2 = $SERVER_CN
IP.1 = 127.0.0.1
EOF

echo "Signing Server Certificate with CA..."
openssl x509 -req -in "$OUTPUT_DIR/server.csr" -CA "$OUTPUT_DIR/ca.crt" -CAkey "$OUTPUT_DIR/ca.key" \
    -CAcreateserial -out "$OUTPUT_DIR/server.crt" -days "$DAYS" -extfile "$SAN_CONFIG"

echo "Generating Client Private Key and CSR..."
CLIENT_SUBJ="/C=$COUNTRY/ST=$STATE/L=$LOCALITY/O=$ORG/OU=$OU/CN=$CLIENT_CN"
openssl genrsa -out "$OUTPUT_DIR/client.key" 2048
openssl req -new -key "$OUTPUT_DIR/client.key" -out "$OUTPUT_DIR/client.csr" -subj "$CLIENT_SUBJ"

# SAN extension config file for client
CLIENT_SAN_CONFIG="$OUTPUT_DIR/client_ext.cnf"
cat <<EOF > "$CLIENT_SAN_CONFIG"
basicConstraints = CA:FALSE
keyUsage = digitalSignature, keyEncipherment
extendedKeyUsage = clientAuth
EOF

echo "Signing Client Certificate with CA..."
openssl x509 -req -in "$OUTPUT_DIR/client.csr" -CA "$OUTPUT_DIR/ca.crt" -CAkey "$OUTPUT_DIR/ca.key" \
    -CAcreateserial -out "$OUTPUT_DIR/client.crt" -days "$DAYS" -extfile "$CLIENT_SAN_CONFIG"

# Cleanup temporary CSR/cnf files
rm -f "$OUTPUT_DIR/server.csr" "$OUTPUT_DIR/client.csr" "$SAN_CONFIG" "$CLIENT_SAN_CONFIG" "$OUTPUT_DIR/ca.srl"

echo "Generating consolidated tls_config.json..."
cat <<EOF > "$OUTPUT_DIR/tls_config.json"
{
  "enabled": true,
  "client_cert": "./client.crt",
  "client_key": "./client.key",
  "ca_cert": "./ca.crt",
  "server_name": "$SERVER_CN",
  "min_version": "1.2",
  "insecure_skip_verify": false
}
EOF

echo "Verifying certificates..."
openssl verify -CAfile "$OUTPUT_DIR/ca.crt" "$OUTPUT_DIR/server.crt"
openssl verify -CAfile "$OUTPUT_DIR/ca.crt" "$OUTPUT_DIR/client.crt"

echo ""
echo "============================================================"
echo "✅ Test Certificates & Consolidated TLS Config Generated!"
echo "Files created in: $OUTPUT_DIR"
echo "  - ca.crt / ca.key (CA PEM)"
echo "  - server.crt / server.key (Server Cert PEM)"
echo "  - client.crt / client.key (Client Cert PEM)"
echo "  - tls_config.json (JISO TLS Config)"
echo "============================================================"
