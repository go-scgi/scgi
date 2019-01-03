# scgi

Package scgi provides a simple scgi client and a number of primitives needed
for basic scgi operation.

## Usage

There are two main ways to use this package. It can be used directly as a
net/http.Client's RoundTripper or it can be added to a net/http.Transport
using RegisterProtocol.
