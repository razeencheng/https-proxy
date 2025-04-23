# HTTPS Proxy User Manual

## Overview

The HTTPS Proxy is a secure proxy server that provides controlled access to web resources through client certificate authentication. It offers:

- HTTPS connection handling
- Client certificate verification
- User activity monitoring
- Administrative dashboard
- Configurable settings
- Bilingual interface (English/Chinese)

## Getting Started

### System Requirements

- Linux (with systemd) or macOS
- Root/sudo access for installation
- Modern web browser for admin interface

### Installation

Please refer to the installation guide in the `deploy/README.md` file for detailed installation instructions.

## Configuration

The configuration is stored in `/etc/https-proxy/config.json` and includes the following main sections:

### Server Settings

```json
"server": {
  "address": "0.0.0.0:8443",
  "cert_file": "/etc/https-proxy/certs/cert.pem",
  "key_file": "/etc/https-proxy/certs/key.pem",
  "language": "en"
}
```

- `address`: The address and port the proxy will listen on
- `cert_file`: Path to the server certificate
- `key_file`: Path to the server private key
- `language`: Interface language (en/zh)

### Proxy Settings

```json
"proxy": {
  "trust_root_file": "/etc/https-proxy/certs/trustroot.pem",
  "auth_required": true
}
```

- `trust_root_file`: Path to the CA certificate for client authentication
- `auth_required`: Whether client certificate authentication is required

### Stats Settings

```json
"stats": {
  "enabled": true,
  "save_interval": 300,
  "file_path": "/opt/https-proxy/stats/proxy_stats.json"
}
```

- `enabled`: Enable/disable statistics collection
- `save_interval`: Interval (in seconds) for saving statistics
- `file_path`: Path where statistics will be saved

### Admin Panel Settings

```json
"admin": {
  "enabled": true,
  "address": "127.0.0.1:8444",
  "cert_file": "/etc/https-proxy/certs/admin_cert.pem",
  "key_file": "/etc/https-proxy/certs/admin_key.pem"
}
```

- `enabled`: Enable/disable admin panel
- `address`: The address and port for the admin panel
- `cert_file`: Admin panel server certificate
- `key_file`: Admin panel server private key

## Using the Proxy

### Client Certificate Setup

1. Obtain a client certificate from your administrator
2. Install the certificate in your browser:
   - Chrome: Settings → Privacy and Security → Security → Manage certificates
   - Firefox: Options → Privacy & Security → Certificates → View Certificates
   - Safari: Preferences → Privacy → Manage Website Data

### Configuring Browsers/Applications

#### Browser Proxy Settings

1. Access proxy settings in your browser
2. Enter the proxy address and port
3. When prompted, select your client certificate

#### System-wide Proxy Settings

- **Windows**: Settings → Network & Internet → Proxy
- **macOS**: System Preferences → Network → Advanced → Proxies
- **Linux**: Settings → Network → Network Proxy

## Admin Panel

### Accessing the Admin Panel

1. Open your browser and navigate to the admin panel address (e.g., https://127.0.0.1:8444)
2. If prompted, select your admin client certificate
3. You will see the dashboard showing user statistics

### Dashboard Features

The dashboard provides:

1. **Overview**:
   - Total request count
   - Total traffic volume
   - Active users
   - Current connections

2. **User Statistics**:
   - List of all users who have connected
   - Traffic usage for each user
   - Request counts
   - Last connection time

3. **User Detail View**:
   - Click on a username to view detailed statistics
   - Historical usage trends
   - Individual request information

### Language Switching

Toggle between English and Chinese using the language selector in the top navigation bar.

## Troubleshooting

### Common Issues

1. **Connection Refused**
   - Verify the proxy is running
   - Check firewall settings
   - Ensure the proxy address is correct

2. **Certificate Errors**
   - Verify the client certificate is properly installed
   - Check that the certificate is issued by the trusted CA
   - Ensure the certificate is not expired

3. **Access Denied**
   - Confirm your client certificate is authorized
   - Check if the proxy requires authentication

### Checking Logs

- **Linux**: `sudo journalctl -u https-proxy`
- **macOS**: Check files in `/var/log/https-proxy/`

## Support

For issues not covered in this manual, please contact your system administrator or file an issue on the project repository.

## Security Considerations

- Keep your client certificate secure
- Do not share your certificate with others
- The proxy logs all access for security purposes
- All traffic through the proxy is monitored

## License

This software is provided under the terms of the MIT License. See the LICENSE file for details. 