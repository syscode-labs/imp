/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Command sandbox-gateway is the node-local data-plane gateway for Imp
// sandboxes. It terminates per-sandbox-token gRPC from SDKs and proxies to
// the guest SandboxControl service over the VM's VSOCK unix socket. It
// needs no Kubernetes API access: HMAC key comes from a file (chart-managed
// Secret) and guest sockets from a read-only hostPath.
package main

import (
	"errors"
	"flag"
	"os"
	"path/filepath"

	"github.com/syscode-labs/imp/internal/sandboxgateway"
)

func main() {
	var (
		socketDir   string
		addr        string
		hmacKeyFile string
		tlsCertFile string
		tlsKeyFile  string
	)
	flag.StringVar(&socketDir, "socket-dir", sandboxgateway.SocketDirDefault, "Directory containing per-VM .vsock unix sockets.")
	flag.StringVar(&addr, "addr", ":9600", "gRPC listen address.")
	flag.StringVar(&hmacKeyFile, "hmac-key-file", "/etc/imp-sandbox-gateway/hmac-key", "File containing the cluster HMAC key for sandbox session tokens.")
	flag.StringVar(&tlsCertFile, "tls-cert-file", "", "TLS certificate file for gRPC. When set, --tls-key-file must also be set; otherwise the gateway serves plaintext.")
	flag.StringVar(&tlsKeyFile, "tls-key-file", "", "TLS private key file for gRPC. When set, --tls-cert-file must also be set.")
	flag.Parse()

	key, err := os.ReadFile(filepath.Clean(hmacKeyFile))
	if err != nil {
		fatal("read hmac key", err)
	}
	if len(key) < 32 {
		fatal("hmac key too short", errors.New("need at least 32 bytes"))
	}

	srv, err := sandboxgateway.NewServer(sandboxgateway.Options{
		SocketDir:   socketDir,
		HMACKey:     key,
		TLSCertFile: tlsCertFile,
		TLSKeyFile:  tlsKeyFile,
	})
	if err != nil {
		fatal("construct server", err)
	}
	if err := srv.ListenAndServe(addr); err != nil {
		fatal("serve", err)
	}
}

func fatal(msg string, err error) {
	msgErr := errors.Join(errors.New(msg), err)
	println(msgErr.Error())
	os.Exit(1)
}
