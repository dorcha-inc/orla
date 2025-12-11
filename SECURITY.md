# Security Policy

We take security vulnerabilities seriously. If you discover a security vulnerability, please follow these steps:

1. Do NOT open a public GitHub issue
2. Do NOT discuss the vulnerability publicly until it has been addressed
3. Email security concerns to hayder@alumni.harvard.edu
   - Include a detailed description of the vulnerability
   - Include steps to reproduce (if applicable)
   - Include potential impact assessment
   - Include suggested fix (if available)

### response timeline

- Initial Response within 2 business days
- Status Update within 7 business days
- The resolution depends on severity and complexity

### disclosure policy

- We will acknowledge receipt of your report within 48 hours
- We will provide regular updates on the status of the vulnerability
- Once the vulnerability is fixed, we will:
  - Credit you (if desired) in the security advisory
  - Publish a security advisory with details
  - Update the changelog

## security best practices

When using orla:

- Keep orla updated to the latest version
- Review and validate tools in your `tools/` directory
- Use appropriate file permissions for tool executables
- Be cautious when executing tools from untrusted sources
- Use configuration files with appropriate access controls
- Monitor logs for suspicious activity

## known security considerations

1. orla executes tools from the filesystem. Ensure tools are from trusted sources
2. tools should have appropriate permissions (executable but not world-writable)
3. when running in HTTP mode, consider firewall rules and authentication
4. ensure timeouts are set appropriately to prevent resource exhaustion

Thank you for helping keep orla secure!
