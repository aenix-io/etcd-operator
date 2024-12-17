package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/dustin/go-humanize"
	"github.com/spf13/cobra"
	clientv3 "go.etcd.io/etcd/client/v3"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/portforward"
	"k8s.io/client-go/transport/spdy"
	"k8s.io/client-go/util/homedir"
)

func main() {
	var rootCmd = &cobra.Command{
		Use:   "kubectl-etcd",
		Short: "Kubectl etcd plugin",
		Long:  `Manage etcd pods spawned by etcd-operator`,
	}

	// Initialize configuration
	config := initializeConfig(rootCmd)

	// Register subcommands
	rootCmd.AddCommand(
		createStatusCmd(config),
		createDefragCmd(config),
		createCompactCmd(config),
		createAlarmCmd(config),
		createForfeitLeadershipCmd(config),
		createLeaveCmd(config),
		createMembersCmd(config),
		createRemoveMemberCmd(config),
		createAddMemberCmd(config),
		createSnapshotCmd(config),
	)

	// Execute the root command
	if err := rootCmd.Execute(); err != nil {
		fmt.Println("Error:", err)
		os.Exit(1)
	}
}

func createStatusCmd(config *Config) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Get the status of etcd cluster member",
		Run: func(cmd *cobra.Command, args []string) {
			etcdClient, err := setupEtcdClient(config)
			if err != nil {
				fmt.Println(err)
				return
			}
			//nolint:errcheck
			defer etcdClient.Close()

			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			status, err := etcdClient.Status(ctx, etcdClient.Endpoints()[0])
			if err != nil {
				fmt.Fprintf(os.Stderr, "Failed to get etcd status: %v\n", err)
				return
			}

			fmt.Printf("%-17s %-9s %-15s %-18s %-11s %-20s %-8s %-s\n",
				"MEMBER", "DB SIZE", "IN USE", "LEADER", "RAFT INDEX", "RAFT APPLIED INDEX", "LEARNER", "ERRORS")
			inUse := fmt.Sprintf("%.2f%%", float64(status.DbSizeInUse)/float64(status.DbSize)*100)
			fmt.Printf("%-17x %-9s %-15s %-18x %-11d %-20d %-8v\n",
				status.Header.MemberId, humanize.Bytes(uint64(status.DbSize)),
				fmt.Sprintf("%s (%s)", humanize.Bytes(uint64(status.DbSizeInUse)), inUse),
				status.Leader, status.RaftIndex, status.RaftAppliedIndex, status.IsLearner)
		},
	}
}

func createDefragCmd(config *Config) *cobra.Command {
	return &cobra.Command{
		Use:   "defrag",
		Short: "Defragment etcd database on the node",
		Long: `Defragmentation is a maintenance operation that compacts the historical
records and optimizes the database storage.`,
		Run: func(cmd *cobra.Command, args []string) {
			etcdClient, err := setupEtcdClient(config)
			if err != nil {
				fmt.Println(err)
				return
			}
			//nolint:errcheck
			defer etcdClient.Close()

			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()

			_, err = etcdClient.Defragment(ctx, etcdClient.Endpoints()[0])
			if err != nil {
				fmt.Fprintf(os.Stderr, "Failed to defragment etcd database: %v\n", err)
				return
			}
		},
	}
}

func createCompactCmd(config *Config) *cobra.Command {
	return &cobra.Command{
		Use:   "compact",
		Short: "Compact the etcd database",
		Long: `Compacts the etcd database up to the latest revision to free up space.
This removes old versions of keys and their associated data.`,
		Run: func(cmd *cobra.Command, args []string) {
			etcdClient, err := setupEtcdClient(config)
			if err != nil {
				fmt.Println("Error setting up etcd client:", err)
				return
			}
			//nolint:errcheck
			defer etcdClient.Close()

			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			// Fetch the latest revision
			statusResp, err := etcdClient.Status(ctx, etcdClient.Endpoints()[0])
			if err != nil {
				fmt.Fprintf(os.Stderr, "Failed to get etcd status: %v\n", err)
				return
			}

			// Compact the etcd database up to the latest revision
			_, err = etcdClient.Compact(ctx, statusResp.Header.Revision)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Failed to compact etcd database: %v\n", err)
				return
			}
		},
	}
}

func createAlarmCmd(config *Config) *cobra.Command {
	alarmCmd := &cobra.Command{
		Use:   "alarm",
		Short: "Manage etcd alarms",
		Long:  `Manage the alarms of an etcd cluster.`,
	}

	alarmCmd.AddCommand(
		createAlarmsListCmd(config),
		createAlarmsDisarmCmd(config),
	)

	return alarmCmd
}

func createAlarmsListCmd(config *Config) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List the etcd alarms for the node",
		Run: func(cmd *cobra.Command, args []string) {
			etcdClient, err := setupEtcdClient(config)
			if err != nil {
				fmt.Println("Error setting up etcd client:", err)
				return
			}
			//nolint:errcheck
			defer etcdClient.Close()

			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			// Call to etcd client to list alarms
			resp, err := etcdClient.AlarmList(ctx)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Failed to list etcd alarms: %v\n", err)
				return
			}

			for _, alarm := range resp.Alarms {
				fmt.Printf("Alarm: %v, MemberID: %x\n", alarm.Alarm, alarm.MemberID)
			}
		},
	}
}

func createAlarmsDisarmCmd(config *Config) *cobra.Command {
	return &cobra.Command{
		Use:   "disarm",
		Short: "Disarm the etcd alarms for the node",
		Run: func(cmd *cobra.Command, args []string) {
			etcdClient, err := setupEtcdClient(config)
			if err != nil {
				fmt.Println("Error setting up etcd client:", err)
				return
			}
			//nolint:errcheck
			defer etcdClient.Close()

			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			// Call to etcd client to disarm alarms
			_, err = etcdClient.AlarmDisarm(ctx, &clientv3.AlarmMember{})
			if err != nil {
				fmt.Fprintf(os.Stderr, "Failed to disarm etcd alarms: %v\n", err)
				return
			}
		},
	}
}

// setupEtcdClient sets up the port forwarding and creates an etcd client.
func setupEtcdClient(config *Config) (*clientv3.Client, error) {
	if config.PodName == "" {
		return nil, fmt.Errorf("You must specify the pod name")
	}

	clientConfig, err := clientcmd.BuildConfigFromFlags("", config.Kubeconfig)
	if err != nil {
		return nil, fmt.Errorf("error building kubeconfig: %s", err)
	}

	clientset, err := kubernetes.NewForConfig(clientConfig)
	if err != nil {
		return nil, fmt.Errorf("error creating Kubernetes client: %s", err)
	}

	tlsConfig, localPort, err := setupPortForwarding(config, clientset)
	if err != nil {
		return nil, fmt.Errorf("failed to setup port forwarding: %s", err)
	}

	etcdConfig := clientv3.Config{
		Endpoints:   []string{fmt.Sprintf("localhost:%d", localPort)},
		DialTimeout: 5 * time.Second,
	}
	if tlsConfig != nil {
		etcdConfig.TLS = tlsConfig
	}

	etcdClient, err := clientv3.New(etcdConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to etcd server: %s", err)
	}

	return etcdClient, nil
}

func createForfeitLeadershipCmd(config *Config) *cobra.Command {
	return &cobra.Command{
		Use:   "forfeit-leadership",
		Short: "Tell node to forfeit etcd cluster leadership",
		Run: func(cmd *cobra.Command, args []string) {
			etcdClient, err := setupEtcdClient(config)
			if err != nil {
				fmt.Println("Error setting up etcd client:", err)
				return
			}
			//nolint:errcheck
			defer etcdClient.Close()

			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			// Retrieve the current status to find the leader
			status, err := etcdClient.Status(ctx, etcdClient.Endpoints()[0])
			if err != nil {
				fmt.Fprintf(os.Stderr, "Failed to get current etcd status: %v\n", err)
				return
			}

			// Retrieve member list to find a member to transfer leadership to
			members, err := etcdClient.MemberList(ctx)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Failed to get etcd member list: %v\n", err)
				return
			}

			for _, member := range members.Members {
				if member.ID != status.Leader {
					_, err = etcdClient.MoveLeader(ctx, member.ID)
					if err != nil {
						fmt.Fprintf(os.Stderr, "Failed to forfeit leadership: %v\n", err)
						return
					}
					return
				}
			}
			fmt.Println("No eligible member found to transfer leadership to or already not the leader.")
		},
	}
}

func createLeaveCmd(config *Config) *cobra.Command {
	return &cobra.Command{
		Use:   "leave",
		Short: "Tell node to leave etcd cluster",
		Run: func(cmd *cobra.Command, args []string) {
			etcdClient, err := setupEtcdClient(config)
			if err != nil {
				fmt.Println("Error setting up etcd client:", err)
				return
			}
			//nolint:errcheck
			defer etcdClient.Close()

			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			// This operation might require administrative privileges on the etcd cluster.
			memberListResp, err := etcdClient.MemberList(ctx)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Failed to retrieve member list: %v\n", err)
				return
			}

			for _, member := range memberListResp.Members {
				if member.Name == config.PodName { // Assuming PodName is set as the member name
					_, err = etcdClient.MemberRemove(ctx, member.ID)
					if err != nil {
						fmt.Fprintf(os.Stderr, "Failed to remove member from cluster: %v\n", err)
						return
					}
					return
				}
			}

			fmt.Println("Specified pod is not a member of the etcd cluster.")
		},
	}
}

func createMembersCmd(config *Config) *cobra.Command {
	return &cobra.Command{
		Use:   "members",
		Short: "Get the list of etcd cluster members",
		Run: func(cmd *cobra.Command, args []string) {
			etcdClient, err := setupEtcdClient(config)
			if err != nil {
				fmt.Println("Error setting up etcd client:", err)
				return
			}
			//nolint:errcheck
			defer etcdClient.Close()

			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			membersResp, err := etcdClient.MemberList(ctx)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Failed to list etcd members: %v\n", err)
				return
			}

			// Header for the table
			fmt.Printf("%-19s %-10s %-30s %-30s %-7s\n", "ID", "HOSTNAME", "PEER URLS", "CLIENT URLS", "LEARNER")
			for _, member := range membersResp.Members {
				fmt.Printf("%-19x %-10s %-30s %-30s %-7v\n",
					member.ID, member.Name, strings.Join(member.PeerURLs, ","), strings.Join(member.ClientURLs, ","), member.IsLearner)
			}
		},
	}
}

func createRemoveMemberCmd(config *Config) *cobra.Command {
	return &cobra.Command{
		Use:   "remove-member <member ID>",
		Short: "Remove a node from the etcd cluster",
		Long:  `Remove a member from the etcd cluster using its member ID.`,
		Args:  cobra.ExactArgs(1), // Ensures exactly one argument is passed
		Run: func(cmd *cobra.Command, args []string) {
			etcdClient, err := setupEtcdClient(config)
			if err != nil {
				fmt.Println("Error setting up etcd client:", err)
				return
			}
			//nolint:errcheck
			defer etcdClient.Close()

			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			// Parse the member ID from the command line argument
			memberID, err := strconv.ParseUint(args[0], 16, 64)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Invalid member ID format: %v\n", err)
				return
			}

			// Remove the member using the provided member ID
			_, err = etcdClient.MemberRemove(ctx, memberID)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Failed to remove member: %v\n", err)
				return
			}
		},
	}
}

func createAddMemberCmd(config *Config) *cobra.Command {
	return &cobra.Command{
		Use:   "add-member [urls]",
		Short: "Add a new member to the etcd cluster",
		Long:  `Add a new member to the etcd cluster using specified peer URLs.`,
		Args:  cobra.ExactArgs(1), // Requires exactly one argument: the new member URL
		Run: func(cmd *cobra.Command, args []string) {
			addMember(config, args[0])
		},
	}
}

func addMember(config *Config, memberURL string) {
	etcdClient, err := setupEtcdClient(config)
	if err != nil {
		fmt.Printf("Failed to set up etcd client: %s\n", err)
		return
	}
	//nolint:errcheck
	defer etcdClient.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	urls := []string{memberURL}
	_, err = etcdClient.MemberAdd(ctx, urls)
	if err != nil {
		fmt.Printf("Failed to add member: %s\n", err)
		return
	}

	fmt.Println("Member successfully added")
}

func createSnapshotCmd(config *Config) *cobra.Command {
	var snapshotCmd = &cobra.Command{
		Use:   "snapshot <path>",
		Short: "Stream snapshot of the etcd node to the path.",
		Long: `Take a snapshot of the etcd database and save it to a specified file path.
This operation is typically used for backup purposes.`,
		Args: cobra.ExactArgs(1), // This command requires exactly one argument for the file path
		Run: func(cmd *cobra.Command, args []string) {
			path := args[0] // The file path where the snapshot will be saved

			etcdClient, err := setupEtcdClient(config)
			if err != nil {
				fmt.Println("Error setting up etcd client:", err)
				return
			}
			//nolint:errcheck
			defer etcdClient.Close()

			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute) // Snapshot can take time
			defer cancel()

			// Requesting a snapshot from the etcd server
			r, err := etcdClient.Snapshot(ctx)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Failed to create snapshot: %v\n", err)
				return
			}
			//nolint:errcheck
			defer r.Close() // Make sure to close the snapshot reader

			// Open the file for writing the snapshot
			f, err := os.Create(path)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Failed to open file %s for writing: %v\n", path, err)
				return
			}
			//nolint:errcheck
			defer f.Close() // Ensure file is closed after writing

			// Copy the snapshot stream to the file
			if _, err = io.Copy(f, r); err != nil {
				fmt.Fprintf(os.Stderr, "Failed to write snapshot to file: %v\n", err)
				return
			}
		},
	}

	// Optional flags can be added here

	return snapshotCmd
}

func setupPortForwarding(config *Config, clientset *kubernetes.Clientset) (*tls.Config, uint16, error) {
	pod, err := clientset.CoreV1().Pods(config.Namespace).Get(context.Background(), config.PodName, metav1.GetOptions{})
	if err != nil {
		return nil, 0, fmt.Errorf("failed to get pod: %w", err)
	}

	path := fmt.Sprintf("/api/v1/namespaces/%s/pods/%s/portforward", config.Namespace, config.PodName)
	clientConfig, err := clientcmd.BuildConfigFromFlags("", config.Kubeconfig)
	if err != nil {
		return nil, 0, fmt.Errorf("error building kubeconfig: %w", err)
	}

	transport, upgrader, err := spdy.RoundTripperFor(clientConfig)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to create round tripper: %w", err)
	}

	hostURL, err := url.Parse(clientConfig.Host)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to parse host URL: %w", err)
	}

	hostURL.Path = path

	stopChan, readyChan := make(chan struct{}, 1), make(chan struct{}, 1)
	dialer := spdy.NewDialer(upgrader, &http.Client{Transport: transport}, "POST", hostURL)

	silentOut := &silentWriter{}
	portForwarder, err := portforward.New(dialer, []string{"0:2379"}, stopChan, readyChan, silentOut, os.Stderr)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to create port forwarder: %w", err)
	}

	// Starting port forwarding
	go func() {
		if err := portForwarder.ForwardPorts(); err != nil {
			fmt.Printf("Failed to start port forwarding: %s\n", err)
		}
	}()

	<-readyChan // Waiting for port forwarding to be ready

	// Obtaining the local port used for forwarding
	forwardedPorts, err := portForwarder.GetPorts()
	if err != nil {
		return nil, 0, fmt.Errorf("failed to get forwarded ports: %w", err)
	}

	localPort := forwardedPorts[0].Local

	tlsConfig, err := getTLSConfig(clientset, pod, config.Namespace)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to get TLS config: %w", err)
	}

	return tlsConfig, localPort, nil
}

// Initialize configuration via Cobra command
func initializeConfig(cmd *cobra.Command) *Config {
	var kubeconfig, namespace, podName string

	// Checking environment variable first
	envKubeconfig := os.Getenv("KUBECONFIG")
	if envKubeconfig != "" {
		kubeconfig = envKubeconfig
	} else {
		// Use default kubeconfig from home directory
		kubeconfig = filepath.Join(homedir.HomeDir(), ".kube", "config")
	}

	// Binding flags
	cmd.PersistentFlags().StringVarP(&kubeconfig, "kubeconfig", "k", kubeconfig, "Path to the kubeconfig file")
	cmd.PersistentFlags().StringVarP(&namespace, "namespace", "n", "",
		"Namespace of the etcd pod (default is the current namespace from kubeconfig)")
	cmd.PersistentFlags().StringVarP(&podName, "pod", "p", "", "Name of the etcd pod")

	// Parse flags
	if err := cmd.ParseFlags(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to parse flags: %v\n", err)
		os.Exit(1)
	}

	// If namespace is not specified, fetch it from kubeconfig context
	if namespace == "" {
		configLoader := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
			clientcmd.NewDefaultClientConfigLoadingRules(),
			&clientcmd.ConfigOverrides{})
		ns, _, err := configLoader.Namespace()
		if err != nil {
			fmt.Printf("Error fetching namespace from kubeconfig: %s\n", err)
			os.Exit(1)
		}
		namespace = ns
		if namespace == "" {
			namespace = "default" // Default to "default" if not specified
		}
	}

	return &Config{
		Kubeconfig: kubeconfig,
		Namespace:  namespace,
		PodName:    podName,
	}
}

// Config struct to hold configuration
type Config struct {
	Kubeconfig string
	Namespace  string
	PodName    string
}

func getTLSConfig(clientset *kubernetes.Clientset, pod *corev1.Pod, namespace string) (*tls.Config, error) {
	for _, container := range pod.Spec.Containers {
		if container.Name == "etcd" {
			secretName, err := findSecretNameForTLS(pod, container)
			if err != nil {
				if err.Error() == "trusted CA file path not specified in container args" {
					return nil, nil
				}
				return nil, err
			}

			caCertPool, clientCert, err := extractTLSFiles(clientset, namespace, secretName)
			if err != nil {
				return nil, err
			}

			return &tls.Config{
				Certificates: []tls.Certificate{*clientCert},
				RootCAs:      caCertPool,
			}, nil
		}
	}
	return nil, fmt.Errorf("etcd container not found")
}

func findSecretNameForTLS(pod *corev1.Pod, container corev1.Container) (string, error) {
	caFilePath := ""
	for _, arg := range append(container.Command, container.Args...) {
		if strings.HasPrefix(arg, "--trusted-ca-file=") {
			caFilePath = strings.TrimPrefix(arg, "--trusted-ca-file=")
			break
		}
	}

	if caFilePath == "" {
		return "", fmt.Errorf("trusted CA file path not specified in container args")
	}

	for _, vm := range container.VolumeMounts {
		if strings.HasPrefix(caFilePath, vm.MountPath) {
			// We found the mount path, now find the volume
			for _, vol := range pod.Spec.Volumes {
				if vol.Name == vm.Name && vol.Secret != nil {
					return vol.Secret.SecretName, nil
				}
			}
		}
	}

	return "", fmt.Errorf("secret for the trusted CA file not found")
}

func extractTLSFiles(clientset *kubernetes.Clientset, namespace, secretName string) (
	*x509.CertPool, *tls.Certificate, error) {
	secret, err := clientset.CoreV1().Secrets(namespace).Get(context.Background(), secretName, metav1.GetOptions{})
	if err != nil {
		return nil, nil, err
	}

	caPem, ok := secret.Data["ca.crt"]
	if !ok {
		return nil, nil, fmt.Errorf("CA certificate not found in secret")
	}
	caCertPool := x509.NewCertPool()
	if !caCertPool.AppendCertsFromPEM(caPem) {
		return nil, nil, fmt.Errorf("failed to parse CA certificate")
	}

	certPem, ok := secret.Data["tls.crt"]
	if !ok {
		return nil, nil, fmt.Errorf("TLS certificate not found in secret")
	}
	keyPem, ok := secret.Data["tls.key"]
	if !ok {
		return nil, nil, fmt.Errorf("TLS key not found in secret")
	}

	clientCert, err := tls.X509KeyPair(certPem, keyPem)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create X509 key pair: %s", err)
	}

	return caCertPool, &clientCert, nil
}

type silentWriter struct{}

func (sw *silentWriter) Write(p []byte) (int, error) {
	return len(p), nil
}
