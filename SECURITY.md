# Security Policy

We take security vulnerabilities seriously. If you discover a security vulnerability, please follow these steps:

1. Do NOT open a public GitHub issue.
2. Do NOT discuss the vulnerability publicly until it has been addressed.
3. Email security concerns to hayder@alumni.harvard.edu.
   - Include a detailed description of the vulnerability.
   - Include steps to reproduce when applicable.
   - Include a potential impact assessment.
   - Include a suggested fix when available.

### response timeline

- Initial response within 2 business days.
- Status update within 7 business days.
- Resolution depends on severity and complexity.

### disclosure policy

- We will acknowledge receipt of your report within 48 hours.
- We will provide regular updates on the status of the vulnerability.
- Once the vulnerability is fixed, we will:
  - Credit you in the security advisory when you wish.
  - Publish a security advisory with details.

## security best practices

When deploying orla:

- Keep orla updated to the latest version.
- Run orla behind an authenticating reverse proxy such as nginx, Cloudflare, or a service mesh. Orla itself does not enforce authentication on its API.
- Use Postgres credentials with the minimum privileges orla requires. Read and write on its own database is enough, and no superuser is needed.
- Protect the upstream LLM and tool API keys orla holds in environment variables. Restrict who can read the process environment, and avoid committing `.env` files.
- Monitor `/metrics` and structured logs for anomalous request patterns.

## known security considerations

1. **No built-in auth.** The HTTP API trusts every caller. A reverse proxy or service mesh must enforce identity, and must also authorize by path: `/api/v1/*` (control plane, backends, stages, mappings, scheduler policy) should generally be restricted to platform-engineer callers, separately from `/v1/*` (data plane). Authenticating the caller alone does not stop a data-plane caller from reaching control-plane routes, since orla itself applies no per-path authorization.
2. **API key fan-out.** Orla holds outbound credentials for every backend. Compromise of the orla process exposes all of them.
3. **Rate limiting is per-instance.** `rate_per_second` is enforced per orla process. Multiple replicas multiply the effective cap.
4. **Tenant isolation is advisory.** Tenancy is carried by request headers such as `X-Orla-Tag-Tenant` and used for fair-share scheduling. It is not a security boundary on its own.

Thank you for helping keep orla secure!
