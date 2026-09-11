package controller

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"io"
	"math/big"
	"net"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	platformv1alpha1 "github.com/molejo-platform/molejo/packages/kubernetes-api/apis/platform/v1alpha1"
	"github.com/molejo-platform/molejo/packages/kubernetesbinding"
	"github.com/molejo-platform/molejo/packages/kubernetespublication"
)

func TestLocalGatewayTLSConformance(t *testing.T) {
	path := os.Getenv("MOLEJO_PUBLICATION_KUBECONFIG")
	if path == "" {
		t.Skip("run tools/testing/publication-kind.sh for the disposable local TLS proof")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Minute)
	defer cancel()
	config, err := clientcmd.BuildConfigFromFlags("", path)
	if err != nil {
		t.Fatal(err)
	}
	admin, err := client.New(config, client.Options{Scheme: testScheme})
	if err != nil {
		t.Fatal(err)
	}
	if err := rbacv1.AddToScheme(testScheme); err != nil {
		t.Fatal(err)
	}
	ns := "publication-app"
	create := func(obj client.Object) {
		t.Helper()
		if err := admin.Create(ctx, obj); err != nil {
			t.Fatal(err)
		}
	}
	create(&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: ns, Labels: map[string]string{"platform.molejo.dev/http-publication": "enabled"}}})
	create(&corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Name: "operator", Namespace: ns}})
	subject := []rbacv1.Subject{{Kind: "ServiceAccount", Name: "operator", Namespace: ns}}
	create(&rbacv1.RoleBinding{ObjectMeta: metav1.ObjectMeta{Name: "runtime", Namespace: ns}, RoleRef: rbacv1.RoleRef{APIGroup: rbacv1.GroupName, Kind: "ClusterRole", Name: "molejo-platform-operator-runtime"}, Subjects: subject})
	create(&rbacv1.ClusterRoleBinding{ObjectMeta: metav1.ObjectMeta{Name: "publication-discovery"}, RoleRef: rbacv1.RoleRef{APIGroup: rbacv1.GroupName, Kind: "ClusterRole", Name: "molejo-platform-operator-discovery"}, Subjects: subject})
	roots, certificate, key := publicationTestCertificate(t)
	create(&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "publication-tls", Namespace: "publication-edge"}, Type: corev1.SecretTypeTLS, Data: map[string][]byte{"tls.crt": certificate, "tls.key": key}})
	mode := gatewayv1.TLSModeTerminate
	from := gatewayv1.NamespacesFromSelector
	gateway := &gatewayv1.Gateway{ObjectMeta: metav1.ObjectMeta{Name: "external", Namespace: "publication-edge"}, Spec: gatewayv1.GatewaySpec{GatewayClassName: "publication-edge"}}
	for i, hostname := range []string{"example.test", "*.example.test"} {
		name := gatewayv1.SectionName("apex")
		if i == 1 {
			name = "pool"
		}
		h := gatewayv1.Hostname(hostname)
		gateway.Spec.Listeners = append(gateway.Spec.Listeners, gatewayv1.Listener{Name: name, Hostname: &h, Port: 8443, Protocol: gatewayv1.HTTPSProtocolType, TLS: &gatewayv1.ListenerTLSConfig{Mode: &mode, CertificateRefs: []gatewayv1.SecretObjectReference{{Name: "publication-tls"}}}, AllowedRoutes: &gatewayv1.AllowedRoutes{Namespaces: &gatewayv1.RouteNamespaces{From: &from, Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"platform.molejo.dev/http-publication": "enabled"}}}}})
	}
	create(gateway)
	app := newAppDeployment(ns, "ap-localtls", os.Getenv("MOLEJO_PUBLICATION_IMAGE"))
	app.Spec.ConfigMapRef = ""
	app.Spec.SecretRef = ""
	app.Spec.PublicEndpoints = []platformv1alpha1.AppDeploymentPublicEndpoint{testHTTPEndpoint("example.test", "www.example.test")}
	for i := range app.Spec.PublicEndpoints[0].Addresses {
		d := &app.Spec.PublicEndpoints[0].Addresses[i].Destination
		d.GatewayNamespace = gateway.Namespace
		d.GatewayName = gateway.Name
		d.SectionName = "apex"
		if i == 1 {
			d.SectionName = "pool"
		}
	}
	create(app)
	limitedConfig := rest.CopyConfig(config)
	limitedConfig.Impersonate.UserName = "system:serviceaccount:" + ns + ":operator"
	limited, err := client.New(limitedConfig, client.Options{Scheme: testScheme})
	if err != nil {
		t.Fatal(err)
	}
	if err := limited.Get(ctx, client.ObjectKey{Namespace: gateway.Namespace, Name: "publication-tls"}, &corev1.Secret{}); !apierrors.IsForbidden(err) {
		t.Fatalf("operator Secret read should be forbidden: %v", err)
	}
	copy := gateway.DeepCopy()
	copy.Labels = map[string]string{"forbidden": "true"}
	if err := limited.Update(ctx, copy); !apierrors.IsForbidden(err) {
		t.Fatalf("operator Gateway mutation should be forbidden: %v", err)
	}
	r := &AppDeploymentReconciler{Client: limited, Scheme: testScheme}
	transport := &http.Transport{TLSClientConfig: &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}, DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, "127.0.0.1:"+os.Getenv("MOLEJO_PUBLICATION_PORT"))
	}}
	defer transport.CloseIdleConnections()
	httpClient := &http.Client{Transport: transport, Timeout: 3 * time.Second}
	fetch := func(host string) (int, string, error) {
		response, err := httpClient.Get("https://" + host + "/")
		if err != nil {
			return 0, "", err
		}
		defer response.Body.Close()
		body, err := io.ReadAll(response.Body)
		return response.StatusCode, string(body), err
	}
	await := func(check func() bool) {
		t.Helper()
		for {
			if check() {
				return
			}
			select {
			case <-ctx.Done():
				t.Fatal("local Gateway/TLS did not converge")
			case <-time.After(200 * time.Millisecond):
			}
		}
	}
	await(func() bool {
		_, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(app)})
		if err != nil {
			return false
		}
		for _, host := range []string{"example.test", "www.example.test"} {
			status, body, err := fetch(host)
			if err != nil || status != 200 || !strings.Contains(body, "molejo conformance") {
				return false
			}
		}
		return true
	})
	t.Log("Both Host/SNI names serve the same application with a trusted matching certificate")
	// Existing external Gateway can be inspected without Helm ownership or TLS material.
	destination := kubernetesbinding.HTTPDestination{BindingID: "binding-one", BindingRevision: 1, SchemaVersion: kubernetesbinding.HTTPBindingSchemaVersion, GatewayNamespace: gateway.Namespace, GatewayName: gateway.Name, SectionName: "apex"}
	facts, err := kubernetespublication.Inspect(ctx, admin, destination, "example.test", ns)
	if err != nil || facts.GatewayUID == "" {
		t.Fatalf("external inspection: %+v %v", facts, err)
	}
	if err := admin.Get(ctx, client.ObjectKeyFromObject(app), app); err != nil {
		t.Fatal(err)
	}
	app.Spec.PublicEndpoints[0].Addresses = app.Spec.PublicEndpoints[0].Addresses[1:]
	if err := admin.Update(ctx, app); err != nil {
		t.Fatal(err)
	}
	await(func() bool {
		if _, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(app)}); err != nil {
			return false
		}
		status, _, err := fetch("example.test")
		remaining, body, otherErr := fetch("www.example.test")
		return err == nil && status == 404 && otherErr == nil && remaining == 200 && strings.Contains(body, "molejo conformance")
	})
	t.Log("Removing apex preserves the second address; Operator can manage routes but cannot mutate Gateway or read keys")
}

func publicationTestCertificate(t *testing.T) (*x509.CertPool, []byte, []byte) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "publication local test"}, DNSNames: []string{"example.test", "www.example.test"}, NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, IsCA: true, BasicConstraintsValid: true}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	certificate := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	encoded, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	roots.AppendCertsFromPEM(certificate)
	return roots, certificate, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: encoded})
}
