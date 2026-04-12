package certgen

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"time"

	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

const (
	tlsCertFile = "tls.crt"
	tlsKeyFile  = "tls.key"
	caCertFile  = "ca.crt"
)

// GenerateCertificates generates CA and server certificates
func GenerateCertificates(hosts []string) (caCert, serverCert, serverKey []byte, err error) {
	// 生成 CA 证书
	ca := &x509.Certificate{
		SerialNumber: big.NewInt(2026),
		Subject: pkix.Name{
			Organization: []string{"DynaStub Operator"},
		},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().AddDate(100, 0, 0),
		IsCA:                  true,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth, x509.ExtKeyUsageServerAuth},
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		BasicConstraintsValid: true,
	}

	caPrivKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, nil, nil, err
	}

	caBytes, err := x509.CreateCertificate(rand.Reader, ca, ca, &caPrivKey.PublicKey, caPrivKey)
	if err != nil {
		return nil, nil, nil, err
	}

	caCert = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caBytes})

	// 生成服务器证书
	server := &x509.Certificate{
		SerialNumber: big.NewInt(2026),
		Subject: pkix.Name{
			Organization: []string{"DynaStub Operator"},
		},
		NotBefore:   time.Now(),
		NotAfter:    time.Now().AddDate(100, 0, 0),
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		KeyUsage:    x509.KeyUsageDigitalSignature,
		DNSNames:    hosts,
	}

	serverPrivKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, nil, nil, err
	}

	serverBytes, err := x509.CreateCertificate(rand.Reader, server, ca, &serverPrivKey.PublicKey, caPrivKey)
	if err != nil {
		return nil, nil, nil, err
	}

	serverCert = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: serverBytes})
	serverKey = pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(serverPrivKey)})

	return caCert, serverCert, serverKey, nil
}

// CreateOrUpdateSecret creates or updates a secret with the certificates
func CreateOrUpdateSecret(client kubernetes.Interface, namespace, secretName string, caCert, serverCert, serverKey []byte) error {
	secret, err := client.CoreV1().Secrets(namespace).Get(context.TODO(), secretName, metav1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			// Create new secret
			secret = &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name: secretName,
				},
				Data: map[string][]byte{
					tlsCertFile: serverCert,
					tlsKeyFile:  serverKey,
					caCertFile:  caCert,
				},
				Type: corev1.SecretTypeTLS,
			}
			_, err = client.CoreV1().Secrets(namespace).Create(context.TODO(), secret, metav1.CreateOptions{})
			return err
		}
		return err
	}

	// Update existing secret
	secret.Data[tlsCertFile] = serverCert
	secret.Data[tlsKeyFile] = serverKey
	secret.Data[caCertFile] = caCert
	_, err = client.CoreV1().Secrets(namespace).Update(context.TODO(), secret, metav1.UpdateOptions{})
	return err
}

// CertExistsAndValid checks if certificate exists and is valid
func CertExistsAndValid(client kubernetes.Interface, namespace, secretName string) (bool, error) {
	secret, err := client.CoreV1().Secrets(namespace).Get(context.TODO(), secretName, metav1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			return false, nil
		}
		return false, err
	}

	if secret.Data == nil || len(secret.Data[tlsCertFile]) == 0 || len(secret.Data[tlsKeyFile]) == 0 {
		return false, nil
	}

	return true, nil
}

// ReadCaCertFromSecret reads CA certificate from existing Secret
func ReadCaCertFromSecret(client kubernetes.Interface, namespace, secretName string) ([]byte, error) {
	secret, err := client.CoreV1().Secrets(namespace).Get(context.TODO(), secretName, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}

	if secret.Data == nil {
		return nil, fmt.Errorf("secret data is empty")
	}

	caCert, ok := secret.Data[caCertFile]
	if !ok || len(caCert) == 0 {
		return nil, fmt.Errorf("CA certificate not found in secret")
	}

	return caCert, nil
}

// CreateOrUpdateWebhookConfiguration creates or updates MutatingWebhookConfiguration
func CreateOrUpdateWebhookConfiguration(client kubernetes.Interface, webhookName, namespace, serviceName string, caCert []byte) error {
	webhook, err := client.AdmissionregistrationV1().MutatingWebhookConfigurations().Get(context.TODO(), webhookName, metav1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			// Create new webhook configuration
			webhook = &admissionregistrationv1.MutatingWebhookConfiguration{
				ObjectMeta: metav1.ObjectMeta{
					Name: webhookName,
					Labels: map[string]string{
						"app.kubernetes.io/managed-by": "dynastub-operator",
					},
				},
				Webhooks: []admissionregistrationv1.MutatingWebhook{
					{
						Name: "mpod.kb.io",
						ClientConfig: admissionregistrationv1.WebhookClientConfig{
							Service: &admissionregistrationv1.ServiceReference{
								Name:      serviceName,
								Namespace: namespace,
								Path:      strPtr("/mutate-v1-pod"),
								Port:      int32Ptr(443),
							},
							CABundle: caCert,
						},
						Rules: []admissionregistrationv1.RuleWithOperations{
							{
								Operations: []admissionregistrationv1.OperationType{
									admissionregistrationv1.Create,
									admissionregistrationv1.Update,
								},
								Rule: admissionregistrationv1.Rule{
									APIGroups:   []string{""},
									APIVersions: []string{"v1"},
									Resources:   []string{"pods"},
									Scope:       scopePtr(admissionregistrationv1.NamespacedScope),
								},
							},
						},
						NamespaceSelector:       &metav1.LabelSelector{},
						FailurePolicy:           failurePolicyPtr(admissionregistrationv1.Ignore),
						SideEffects:             sideEffectPtr(admissionregistrationv1.SideEffectClassNone),
						AdmissionReviewVersions: []string{"v1"},
						TimeoutSeconds:          int32Ptr(30),
					},
				},
			}
			_, err = client.AdmissionregistrationV1().MutatingWebhookConfigurations().Create(context.TODO(), webhook, metav1.CreateOptions{})
			return err
		}
		return err
	}

	// Update existing webhook configuration
	for i := range webhook.Webhooks {
		webhook.Webhooks[i].ClientConfig.CABundle = caCert
	}
	_, err = client.AdmissionregistrationV1().MutatingWebhookConfigurations().Update(context.TODO(), webhook, metav1.UpdateOptions{})
	return err
}

// DeleteWebhookConfiguration deletes MutatingWebhookConfiguration if it's managed by us
func DeleteWebhookConfiguration(client kubernetes.Interface, webhookName string) error {
	webhook, err := client.AdmissionregistrationV1().MutatingWebhookConfigurations().Get(context.TODO(), webhookName, metav1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			return nil
		}
		return err
	}

	if webhook.Labels["app.kubernetes.io/managed-by"] != "dynastub-operator" {
		return nil
	}

	return client.AdmissionregistrationV1().MutatingWebhookConfigurations().Delete(context.TODO(), webhookName, metav1.DeleteOptions{})
}

// Helper functions
func strPtr(s string) *string {
	return &s
}

func int32Ptr(i int32) *int32 {
	return &i
}

func scopePtr(s admissionregistrationv1.ScopeType) *admissionregistrationv1.ScopeType {
	return &s
}

func failurePolicyPtr(f admissionregistrationv1.FailurePolicyType) *admissionregistrationv1.FailurePolicyType {
	return &f
}

func sideEffectPtr(s admissionregistrationv1.SideEffectClass) *admissionregistrationv1.SideEffectClass {
	return &s
}
