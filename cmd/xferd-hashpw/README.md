# xferd-hashpw - Password Hash Generator

A utility to generate bcrypt password hashes for use with xferd's basic authentication.

## Usage

```bash
xferd-hashpw
```

You will be prompted to enter a password (input is hidden for security). The utility will generate a bcrypt hash that you can use in your `config.yml`.

## Example

```bash
$ xferd-hashpw
xferd Password Hash Generator
==============================

Enter password: [hidden]

Generated bcrypt hash:
$2a$10$N9qo8uLOickgx2ZMRZoMyeIjZAgcfl7p92ldGxad68LJZdL17lhWy

Add this to your config.yml:

server:
  basic_auth:
    enabled: true
    username: your_username
    password_hash: "$2a$10$N9qo8uLOickgx2ZMRZoMyeIjZAgcfl7p92ldGxad68LJZdL17lhWy"

Note: Do NOT use both 'password' and 'password_hash' - use only 'password_hash' for production.
```

## Building from Source

```bash
go build -o xferd-hashpw ./cmd/xferd-hashpw
```

## Security Notes

- Passwords are never echoed to the terminal
- Uses bcrypt with default cost factor (10)
- Each hash is unique due to random salt
- Hashes are safe to store in configuration files
- Always use `password_hash` instead of `password` in production

## Why Use Password Hashes?

**Security Benefits:**
- Passwords are not stored in plaintext
- If configuration file is compromised, passwords cannot be recovered
- Bcrypt is designed to be slow, making brute-force attacks impractical
- Each password has a unique salt

**Best Practices:**
- Generate a new hash when changing passwords
- Use strong passwords (12+ characters, mixed case, numbers, symbols)
- Rotate passwords regularly
- Never commit password hashes to version control
- Secure configuration files with `chmod 600`






