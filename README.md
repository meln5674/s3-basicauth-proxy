# S3 Basicauth Proxy

This webapp is designed to proxy any number of AWS S3 (or compatible) buckets, across any number of regions and/or services. Instead of implementing the AWS S3 API, the tool attempts to present a "standard" HTTP server url structure, and use "Authorization: Basic ..." headers for authentication. This lets legacy tools designed to make use of basic HTTP operations and authentication to use S3 buckets for backends. The tool is entirely stateless, so multiple replicas can be deployed without any additional consideration.

## Supported Operations

### `POST,PUT /<endpoint>/<region>/<bucket>` - Create bucket

### `POST,PUT /<endpoint>/<region>/<bucket>/<key>` - Upload object

### `GET,HEAD /<endpoint>/<region>/` - List buckets in `region`

### `GET,HEAD /<endpoint>/<region>/<bucket>/` - List objects in `bucket`

### `GET,HEAD /<endpoint/<region>/<bucket>/<prefix>/` - List objects in `bucket` whose keys start with `prefix`

### `GET,HEAD /<endpoint>/<region>/<bucket>/<key>` - Download object

### `DELETE /<endpoint>/<region>/<bucket>` - Delete bucket

### `DELETE /<endpoint>/<region>/<bucket>/<key>` - Delete object

### `DELETE /<endpoint>/<region>/<prefix>/` - Delete all objects in `bucket` whose keys start with `prefix`

## Building/Running

```bash
make bin/proxy
bin/proxy [-listen-addr 127.0.0.1] [-listen-port 8080] [-tls-key-path /path/to/tls.key -tls-cert-path /path/to/tlk.crt]
```

