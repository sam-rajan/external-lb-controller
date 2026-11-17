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
	"os"
	"os/exec"
	"strings"

	. "github.com/onsi/gomega"

	"github.com/sam-rajan/external-lb-controller/test/utils"
)

// The manager exits unless a cloud provider is detected, and the AWS provider needs a working
// sts:GetCallerIdentity. Kind has no AWS account, so the suite fakes that one STS call.

const (
	fakeSTSName = "fake-sts"
	fakeSTSPort = 8080
	managerName = "external-lb-controller-controller-manager"
)

// fakeSTSImage is built locally and loaded into Kind; pulling one is rate limited (429) on CI runners.
const fakeSTSImage = "example.com/fake-sts:v0.0.1"

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
        image: example.com/fake-sts:v0.0.1
        imagePullPolicy: IfNotPresent
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

// deployFakeSTS starts the fake STS and points the manager at it, which triggers a rollout.
func deployFakeSTS() {
	containerTool := "docker"
	if v, ok := os.LookupEnv("CONTAINER_TOOL"); ok {
		containerTool = v
	}
	cmd := exec.Command(containerTool, "build", "-t", fakeSTSImage, "test/e2e/fakests")
	_, err := utils.Run(cmd)
	ExpectWithOffset(1, err).NotTo(HaveOccurred(), "Failed to build the fake STS image")

	ExpectWithOffset(1, utils.LoadImageToKindClusterWithName(fakeSTSImage)).To(Succeed(),
		"Failed to load the fake STS image into Kind")

	cmd = exec.Command("kubectl", "apply", "-f", "-")
	cmd.Stdin = strings.NewReader(fakeSTSManifest)
	_, err = utils.Run(cmd)
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
