#!/bin/bash

# 在远程服务器上执行此脚本生成正确的 webhook 证书

set -e

CERT_DIR="${CERT_DIR:-./certs}"
SERVICE_NAME="${SERVICE_NAME:-k8s-http-fake-operator-webhook}"
NAMESPACE="${NAMESPACE:-default}"

echo "Generating server certificate for ${SERVICE_NAME}.${NAMESPACE}.svc"

# 创建证书目录
mkdir -p "${CERT_DIR}"

# 使用现有的 CA 证书和密钥
cat > "${CERT_DIR}/ca.crt" <<'EOF'
-----BEGIN CERTIFICATE-----
MIIDZTCCAk2gAwIBAgIUHhylu88nJbBIH01guZB5YJkm/sswDQYJKoZIhvcNAQEL
BQAwQjEfMB0GA1UEAwwWazhzLWh0dHAtZmFrZS1vcGVyYXRvcjEfMB0GA1UECgwW
azhzLWh0dHAtZmFrZS1vcGVyYXRvcjAeFw0yNjAzMjkwMjM4MjlaFw0zNjAzMjYw
MjM4MjlaMEIxHzAdBgNVBAMMFms4cy1odHRwLWZha2Utb3BlcmF0b3IxHzAdBgNV
BAoMFms4cy1odHRwLWZha2Utb3BlcmF0b3IwggEiMA0GCSqGSIb3DQEBAQUAA4IB
DwAwggEKAoIBAQCmoMtEvDmQJPPZibHlUTcY6qqLXgEFACUsU+Jx1ThJAxrBScKj
K2ozsj9tHvGncWYL2ENVbNh1xN4qBiAfi9+q40TnHp0hknVxiR4nBDdS2vU1uAJz
ECISYiXWj+z/WO/grveqJB+v6Yim8H3clFGUzOIj+P6gX0Wx2txUcJUYBEUx2IBB
CELpzXwKe0/zApXkPK9ENBrnx49+8JQHYQPrEaziOzY8uqkNnivBBeCZKM9LtRTL
c2+JCO61Qrky+eINmJs0D36SkZeIXcmF2AT/qgMnvkGtRe8L7gUBxvbAYj1bbyth
HYWm+A8Cv6MzMLmuMU2sAyR5+s+2tuHQn7H/AgMBAAGjUzBRMB0GA1UdDgQWBBT2
/voD7ylEXEbh0o8A7jWQOQ/nizAfBgNVHSMEGDAWgBT2/voD7ylEXEbh0o8A7jWQ
OQ/nizAPBgNVHRMBAf8EBTADAQH/MA0GCSqGSIb3DQEBCwUAA4IBAQApJsfkOrQ8
bGled4/7IXXdcQewyrmOrelTieRo/MMlmYthx69HeUU5tqr1Wfjqd986JX6/Q7wm
Z9h4NcQgYtsQmGRnDNExB/hFAXa3so+dpk2rHu3jqgdJe1zk451yQfLOuWb0cwBo
sEdQqw0xXV89d+FMSmQSU4B46kny6AAAMv36pVW1OguwnFXnL2pOAm7bcWrdd7vh
LFAlu7A7c93XWZaeObVwycBMTapA+U8wmktl86jdPVmwVXoHSP0b5aQliV1ly5QO
f56kbKjOYlDAyqOyBLboXoIKCO/R5BYBkUNS7deQcehINxeBkQUUJ/dGIM6ggAtr
cva1JAa0aLni
-----END CERTIFICATE-----
EOF

cat > "${CERT_DIR}/ca.key" <<'EOF'
-----BEGIN PRIVATE KEY-----
MIIEvQIBADANBgkqhkiG9w0BAQEFAASCBKcwggSjAgEAAoIBAQCmoMtEvDmQJPPZ
ibHlUTcY6qqLXgEFACUsU+Jx1ThJAxrBScKjK2ozsj9tHvGncWYL2ENVbNh1xN4q
BiAfi9+q40TnHp0hknVxiR4nBDdS2vU1uAJzECISYiXWj+z/WO/grveqJB+v6Yim
8H3clFGUzOIj+P6gX0Wx2txUcJUYBEUx2IBBCELpzXwKe0/zApXkPK9ENBrnx49+
8JQHYQPrEaziOzY8uqkNnivBBeCZKM9LtRTLc2+JCO61Qrky+eINmJs0D36SkZeI
XcmF2AT/qgMnvkGtRe8L7gUBxvbAYj1bbythHYWm+A8Cv6MzMLmuMU2sAyR5+s+2
tuHQn7H/AgMBAAECggEAEtBuMzJAvtWtWzS2NTiEWdgkQbnL6E9BeB8KwXBXH5DA
jeVj+7A2EN5Ppg2/kqJVMW+Mq6u6Yx9VewyIQsx5hOvO7QWX2r+nRcUtN4R+SLPj
c+0hxQ7H69hKlSvOsaO3cdUzldZbwgCzNfbq7PND55Yj2OjdAKDspIgv1X2E6f9W
vSxQG3wkNAFE5VCkhd4qhI/Pzi+oX/UqMN0XcKwXWzKrJGZbGxQKjTczbmhS2pPL
UWQ/XVuxlEjcbYI9c3/jWjSRGqA69S373Hwt96KPXJRvP5jXE4ynPGnlL684/LWD
H7zMZnytCxjx3IV+Yq4uB88F3YMzs19YJungwIcP4QKBgQDcDQio/zx3wcqs0OAx
6+hcUuiS3HUkvcZ5YQ+w62q8JdciSzKfGIYvaddF3jDBG1X5I+PpdFhR240HiBut
roxQW+zvQBxfDokor30IEJcDIkQWb0mJUKwMkXsYY0FZG6Z7BwToQbpWPzf3Xuoz
pq6He0IZEAU7n6t1txLdk0GtjwKBgQDB2YMZ9nQS60YC1PmGRqkEZt4+bfNKqiZP
B8Mo97FppvFD8744LjRxh6ry4Z3uyMVtwkzKSUM6upo3QYvQ0c1onDUmKvl9fZ1s
sss4qLkDPRG96zIBjt5vVt3cW+IJpo8OUmG9tzNQ7LkUumeKf/1Hg9ucnVO8xfDX
TiZI9KNckQKBgHYOKA9Cn9ZACdQdW6psvgSKFmx0CgTkK48DG7/3DRRT2M91OHtS
VOsrBWtegRmY6M75ClU9LgT8nPTleLP9aRnTt5HD+3Sj97Om0pV5EQuFXrIKkpEw
zp0Pj9LNrUl5JB/s7BO5kFPOV9ldJCxZAEBh6KajbQnPX2x8lUdo6bREAoGAIrIv
/Cm1p20kr9Qu2EEiQwyorOkSa6RfjSeGyOi5bbQnSEJfJ31JpA3/qS4gZsDJ7+WF
3/WhLOq6DtOfv1gk++jn8UPon9y6vdxbLdZ5k5kXkatOEJUQMDZwoDcrqdRxHepE
wKMX4Af4Kf6qoXaKsiakxM7pwrvj14q9b+JiS2hAoGBAM6ui7MSV4H/BnQxZ+WH
YL4H+Ls4sx8oMwJ/newWyUIBAOT7Si+WEri6QC0eFvmlX+Kg722BHi9WOTmDyC0L
OyKt6vnub73MGLrdM8CezJ77WMiK1UCdrVTJhnkF76Ht/KjfhnyAqKM6mGoCpL27
O0jw0cLobo1ICDt632Cf6ik7
-----END PRIVATE KEY-----
EOF

# 生成服务器私钥
openssl genrsa -out "${CERT_DIR}/tls.key" 2048

# 创建 CSR 配置文件
cat > "${CERT_DIR}/csr.conf" <<EOF
[req]
req_extensions = v3_req
distinguished_name = req_distinguished_name
[req_distinguished_name]
[v3_req]
basicConstraints = CA:FALSE
keyUsage = nonRepudiation, digitalSignature, keyEncipherment
extendedKeyUsage = serverAuth
subjectAltName = @alt_names
[alt_names]
DNS.1 = ${SERVICE_NAME}
DNS.2 = ${SERVICE_NAME}.${NAMESPACE}
DNS.3 = ${SERVICE_NAME}.${NAMESPACE}.svc
DNS.4 = ${SERVICE_NAME}.${NAMESPACE}.svc.cluster.local
EOF

# 生成 CSR
openssl req -new -key "${CERT_DIR}/tls.key" \
  -subj "/CN=${SERVICE_NAME}.${NAMESPACE}.svc" \
  -config "${CERT_DIR}/csr.conf" \
  -out "${CERT_DIR}/tls.csr"

# 使用 CA 签名证书
openssl x509 -req -in "${CERT_DIR}/tls.csr" \
  -CA "${CERT_DIR}/ca.crt" \
  -CAkey "${CERT_DIR}/ca.key" \
  -CAcreateserial \
  -days 3650 \
  -extensions v3_req \
  -extfile "${CERT_DIR}/csr.conf" \
  -out "${CERT_DIR}/tls.crt"

# 清理临时文件
rm -f "${CERT_DIR}/tls.csr" "${CERT_DIR}/csr.conf" "${CERT_DIR}/ca.srl"

echo ""
echo "Certificates generated successfully in ${CERT_DIR}"
echo ""
echo "=== CA Bundle (for webhook configuration) ==="
base64 -w 0 "${CERT_DIR}/ca.crt"
echo ""
echo ""
echo "=== TLS Certificate (for secret) ==="
base64 -w 0 "${CERT_DIR}/tls.crt"
echo ""
echo ""
echo "=== TLS Key (for secret) ==="
base64 -w 0 "${CERT_DIR}/tls.key"
echo ""
