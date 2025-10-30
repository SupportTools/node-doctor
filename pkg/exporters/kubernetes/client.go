package kubernetes

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8stypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/retry"

	"github.com/supporttools/node-doctor/pkg/types"
)

// K8sClient wraps the Kubernetes clientset with retry logic and node-specific operations
type K8sClient struct {
	clientset kubernetes.Interface
	nodeName  string
	nodeUID   string
}

// NewClient creates a new Kubernetes client with automatic configuration detection
func NewClient(config *types.KubernetesExporterConfig, settings *types.GlobalSettings) (*K8sClient, error) {
	var restConfig *rest.Config
	var err error

	// Auto-detect configuration: kubeconfig file vs in-cluster
	if settings.Kubeconfig != "" {
		log.Printf("[INFO] Using kubeconfig from file: %s", settings.Kubeconfig)
		restConfig, err = clientcmd.BuildConfigFromFlags("", settings.Kubeconfig)
		if err != nil {
			return nil, fmt.Errorf("failed to build config from kubeconfig %s: %w", settings.Kubeconfig, err)
		}
	} else {
		log.Printf("[INFO] Attempting to use in-cluster configuration")
		restConfig, err = rest.InClusterConfig()
		if err != nil {
			return nil, fmt.Errorf("failed to get in-cluster config (are you running inside a pod?): %w", err)
		}
	}

	// Apply QPS and burst settings
	restConfig.QPS = settings.QPS
	restConfig.Burst = settings.Burst

	// Create clientset with retry and timeout
	clientset, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create Kubernetes clientset: %w", err)
	}

	client := &K8sClient{
		clientset: clientset,
		nodeName:  settings.NodeName,
	}

	// Retrieve and cache node UID for event creation
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := client.initializeNodeUID(ctx); err != nil {
		return nil, fmt.Errorf("failed to initialize node UID: %w", err)
	}

	log.Printf("[INFO] Kubernetes client initialized for node %s (UID: %s)", client.nodeName, client.nodeUID)
	return client, nil
}

// initializeNodeUID retrieves and caches the node UID for event creation
func (c *K8sClient) initializeNodeUID(ctx context.Context) error {
	var err error
	err = retry.OnError(
		retry.DefaultBackoff,
		func(err error) bool {
			return errors.IsServiceUnavailable(err) || errors.IsTimeout(err) || errors.IsServerTimeout(err)
		},
		func() error {
			node, err := c.clientset.CoreV1().Nodes().Get(ctx, c.nodeName, metav1.GetOptions{})
			if err != nil {
				return err
			}
			c.nodeUID = string(node.UID)
			return nil
		},
	)

	if err != nil {
		return fmt.Errorf("failed to get node %s: %w", c.nodeName, err)
	}

	return nil
}

// PatchNodeConditions updates node conditions using strategic merge patch with retry logic
func (c *K8sClient) PatchNodeConditions(ctx context.Context, conditions []corev1.NodeCondition) error {
	if len(conditions) == 0 {
		return nil
	}

	log.Printf("[DEBUG] Patching %d node conditions for node %s", len(conditions), c.nodeName)

	// Create patch data for node conditions
	patch := map[string]interface{}{
		"status": map[string]interface{}{
			"conditions": conditions,
		},
	}

	patchBytes, err := jsonMarshal(patch)
	if err != nil {
		return fmt.Errorf("failed to marshal patch data: %w", err)
	}

	// Retry the patch operation
	err = retry.OnError(
		retry.DefaultBackoff,
		func(err error) bool {
			if errors.IsConflict(err) {
				log.Printf("[DEBUG] Node patch conflict, retrying...")
				return true
			}
			return errors.IsServiceUnavailable(err) || errors.IsTimeout(err) || errors.IsServerTimeout(err)
		},
		func() error {
			_, patchErr := c.clientset.CoreV1().Nodes().Patch(
				ctx,
				c.nodeName,
				k8stypes.StrategicMergePatchType,
				patchBytes,
				metav1.PatchOptions{},
				"status",
			)
			return patchErr
		},
	)

	if err != nil {
		return fmt.Errorf("failed to patch node conditions after retries: %w", err)
	}

	log.Printf("[DEBUG] Successfully patched node conditions for node %s", c.nodeName)
	return nil
}

// CreateEvent creates a Kubernetes event with retry logic and error handling
func (c *K8sClient) CreateEvent(ctx context.Context, event corev1.Event, namespace string) error {
	// Set the involved object UID
	event.InvolvedObject.UID = k8stypes.UID(c.nodeUID)
	event.Namespace = namespace

	log.Printf("[DEBUG] Creating event: %s/%s (reason: %s)", namespace, event.Name, event.Reason)

	// Retry the event creation
	err := retry.OnError(
		retry.DefaultBackoff,
		func(err error) bool {
			// Don't retry on AlreadyExists - events are meant to be unique
			if errors.IsAlreadyExists(err) {
				log.Printf("[DEBUG] Event %s already exists, skipping", event.Name)
				return false
			}
			return errors.IsServiceUnavailable(err) || errors.IsTimeout(err) || errors.IsServerTimeout(err)
		},
		func() error {
			_, createErr := c.clientset.CoreV1().Events(namespace).Create(ctx, &event, metav1.CreateOptions{})
			return createErr
		},
	)

	if err != nil {
		if errors.IsAlreadyExists(err) {
			// Not an error - event already exists
			log.Printf("[DEBUG] Event %s already exists", event.Name)
			return nil
		}
		return fmt.Errorf("failed to create event %s after retries: %w", event.Name, err)
	}

	log.Printf("[DEBUG] Successfully created event: %s/%s", namespace, event.Name)
	return nil
}

// GetNode retrieves the current node object with retry logic
func (c *K8sClient) GetNode(ctx context.Context) (*corev1.Node, error) {
	var node *corev1.Node
	var err error

	err = retry.OnError(
		retry.DefaultBackoff,
		func(err error) bool {
			return errors.IsServiceUnavailable(err) || errors.IsTimeout(err) || errors.IsServerTimeout(err)
		},
		func() error {
			node, err = c.clientset.CoreV1().Nodes().Get(ctx, c.nodeName, metav1.GetOptions{})
			return err
		},
	)

	if err != nil {
		return nil, fmt.Errorf("failed to get node %s after retries: %w", c.nodeName, err)
	}

	return node, nil
}

// UpdateNodeAnnotations updates node annotations using patch with retry logic
func (c *K8sClient) UpdateNodeAnnotations(ctx context.Context, annotations map[string]string) error {
	if len(annotations) == 0 {
		return nil
	}

	log.Printf("[DEBUG] Updating %d node annotations for node %s", len(annotations), c.nodeName)

	// Create patch data for annotations
	patch := map[string]interface{}{
		"metadata": map[string]interface{}{
			"annotations": annotations,
		},
	}

	patchBytes, err := jsonMarshal(patch)
	if err != nil {
		return fmt.Errorf("failed to marshal annotation patch data: %w", err)
	}

	// Retry the patch operation
	err = retry.OnError(
		retry.DefaultBackoff,
		func(err error) bool {
			if errors.IsConflict(err) {
				log.Printf("[DEBUG] Node annotation patch conflict, retrying...")
				return true
			}
			return errors.IsServiceUnavailable(err) || errors.IsTimeout(err) || errors.IsServerTimeout(err)
		},
		func() error {
			_, patchErr := c.clientset.CoreV1().Nodes().Patch(
				ctx,
				c.nodeName,
				k8stypes.StrategicMergePatchType,
				patchBytes,
				metav1.PatchOptions{},
			)
			return patchErr
		},
	)

	if err != nil {
		return fmt.Errorf("failed to patch node annotations after retries: %w", err)
	}

	log.Printf("[DEBUG] Successfully updated node annotations for node %s", c.nodeName)
	return nil
}

// ListEvents lists events for the node with optional filtering
func (c *K8sClient) ListEvents(ctx context.Context, namespace string) (*corev1.EventList, error) {
	var events *corev1.EventList
	var err error

	// Use field selector to filter events for this node
	fieldSelector := fmt.Sprintf("involvedObject.name=%s", c.nodeName)

	err = retry.OnError(
		retry.DefaultBackoff,
		func(err error) bool {
			return errors.IsServiceUnavailable(err) || errors.IsTimeout(err) || errors.IsServerTimeout(err)
		},
		func() error {
			events, err = c.clientset.CoreV1().Events(namespace).List(ctx, metav1.ListOptions{
				FieldSelector: fieldSelector,
			})
			return err
		},
	)

	if err != nil {
		return nil, fmt.Errorf("failed to list events for node %s after retries: %w", c.nodeName, err)
	}

	return events, nil
}

// IsHealthy performs a basic health check by trying to get the node
func (c *K8sClient) IsHealthy(ctx context.Context) bool {
	_, err := c.GetNode(ctx)
	return err == nil
}

// GetNodeName returns the configured node name
func (c *K8sClient) GetNodeName() string {
	return c.nodeName
}

// GetNodeUID returns the cached node UID
func (c *K8sClient) GetNodeUID() string {
	return c.nodeUID
}

// jsonMarshal is a helper function to marshal data to JSON using the standard library
func jsonMarshal(v interface{}) ([]byte, error) {
	return json.Marshal(v)
}