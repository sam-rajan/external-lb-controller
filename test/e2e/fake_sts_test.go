//go:build e2e
// +build e2e

/*
Copyright 2026 The external-lb-controller Authors.

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

package e2e

import (
	"fmt"
	"os/exec"
	"strings"

	. "github.com/onsi/gomega"

	"github.com/sam-rajan/external-lb-controller/test/utils"
)

// The AWS provider only counts as detected when sts:GetCallerIdentity succeeds
// (see internal/lb/aws/provider.go), and the manager exits at startup when no
// provider is detected. The Kind cluster used by these tests has no AWS
// account, so the suite runs a tiny in-cluster fake of the one STS call the
// controller makes at startup and points the manager's AWS SDK at it. The
// controller code itself is exercised unchanged.

const (
	fakeSTSName = "fake-sts"
	fakeSTSPort = 8080
	managerName = "external-lb-controller-controller-manager"
)

// fakeSTSManifest is a restricted-policy-compliant Deployment and Service that
// answer every request with a canned GetCallerIdentity response.
const fakeSTSManifest = `apiVersion: apps/v1
kind: Deployment
metadata:
  name: fake-sts
  namespace: external-lb-controller-system
spec:
  replicas: 1
  selector:
    matchLabels:
      app: fake-sts
  template:
    metadata:
      labels:
        app: fake-sts
    spec:
      securityContext:
        runAsNonRoot: true
        runAsUser: 65534
        seccompProfile:
          type: RuntimeDefault
      containers:
      - name: fake-sts
        image: public.ecr.aws/docker/library/python:3.13-alpine
        command: ["python", "-u", "-c"]
        args:
        - |
          from http.server import BaseHTTPRequestHandler, HTTPServer
          BODY = (
              b'<GetCallerIdentityResponse xmlns="https://sts.amazonaws.com/doc/2011-06-15/">'
              b'<GetCallerIdentityResult>'
              b'<Arn>arn:aws:iam::111122223333:user/e2e</Arn>'
              b'<UserId>AIDAEXAMPLEE2E</UserId>'
              b'<Account>111122223333</Account>'
              b'</GetCallerIdentityResult>'
              b'<ResponseMetadata><RequestId>e2e</RequestId></ResponseMetadata>'
              b'</GetCallerIdentityResponse>'
          )
          class Handler(BaseHTTPRequestHandler):
              def do_POST(self):
                  self.rfile.read(int(self.headers.get("Content-Length", 0)))
                  self.send_response(200)
                  self.send_header("Content-Type", "text/xml")
                  self.send_header("Content-Length", str(len(BODY)))
                  self.end_headers()
                  self.wfile.write(BODY)
              do_GET = do_POST
          HTTPServer(("", 8080), Handler).serve_forever()
        ports:
        - containerPort: 8080
        readinessProbe:
          tcpSocket:
            port: 8080
        securityContext:
          allowPrivilegeEscalation: false
          capabilities:
            drop: ["ALL"]
---
apiVersion: v1
kind: Service
metadata:
  name: fake-sts
  namespace: external-lb-controller-system
spec:
  selector:
    app: fake-sts
  ports:
  - port: 8080
    targetPort: 8080
`

// deployFakeSTS starts the fake STS endpoint and configures the already
// deployed manager to use it with dummy credentials. Changing the manager's
// environment triggers a rollout, so the crash-looping pod is replaced.
func deployFakeSTS() {
	cmd := exec.Command("kubectl", "apply", "-f", "-")
	cmd.Stdin = strings.NewReader(fakeSTSManifest)
	_, err := utils.Run(cmd)
	ExpectWithOffset(1, err).NotTo(HaveOccurred(), "Failed to deploy the fake STS endpoint")

	cmd = exec.Command("kubectl", "rollout", "status", "deployment/"+fakeSTSName,
		"-n", namespace, "--timeout=120s")
	_, err = utils.Run(cmd)
	ExpectWithOffset(1, err).NotTo(HaveOccurred(), "Fake STS endpoint did not become ready")

	cmd = exec.Command("kubectl", "set", "env", "deployment/"+managerName, "-n", namespace,
		fmt.Sprintf("AWS_ENDPOINT_URL_STS=http://%s.%s.svc:%d", fakeSTSName, namespace, fakeSTSPort),
		"AWS_REGION=us-east-1",
		"AWS_ACCESS_KEY_ID=e2e",
		"AWS_SECRET_ACCESS_KEY=e2e",
		"AWS_EC2_METADATA_DISABLED=true",
	)
	_, err = utils.Run(cmd)
	ExpectWithOffset(1, err).NotTo(HaveOccurred(), "Failed to point the manager at the fake STS endpoint")
}
