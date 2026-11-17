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

// Command fakests answers every request with a canned STS GetCallerIdentity
// response. The e2e suite runs it in the Kind cluster so the AWS provider can
// be detected without a real AWS account. It is built into its own image by
// test/e2e/fake_sts_test.go and is not part of the controller.
package main

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"time"
)

const response = `<GetCallerIdentityResponse xmlns="https://sts.amazonaws.com/doc/2011-06-15/">` +
	`<GetCallerIdentityResult>` +
	`<Arn>arn:aws:iam::111122223333:user/e2e</Arn>` +
	`<UserId>AIDAEXAMPLEE2E</UserId>` +
	`<Account>111122223333</Account>` +
	`</GetCallerIdentityResult>` +
	`<ResponseMetadata><RequestId>e2e</RequestId></ResponseMetadata>` +
	`</GetCallerIdentityResponse>`

func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "text/xml")
		_, _ = fmt.Fprint(w, response)
	})

	srv := &http.Server{
		Addr:              ":8080",
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}
	log.Fatal(srv.ListenAndServe())
}
