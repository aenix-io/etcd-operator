//go:build e2e

package e2e

import (
	"context"
	"fmt"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	etcdv1alpha2 "github.com/cozystack/etcd-operator/api/v1alpha2"
)

const (
	rebootstrapNamespace = "seed-rebootstrap-e2e"
	rebootstrapCluster   = "etcd"
	rebootstrapKey       = "/e2e/pre-wipe-sentinel"
	rebootstrapValue     = "written-before-the-wipe"
)

// TestSeedDataDirLossDoesNotRebootstrap covers the one data-loss shape that a
// crash-loop cannot express, and that the seed used to be uniquely vulnerable
// to.
//
// A member Pod is built with --initial-cluster-state, which etcd honours only
// when the data dir is empty. For a non-seed member that flag is `existing`, so
// an empty data dir fails loudly against a stale --initial-cluster and the
// member crash-loops into self-heal. The seed used to carry `new` for the life
// of the cluster, and `new` against its frozen self-only --initial-cluster is a
// complete bootstrap instruction: etcd does not error, it forms a *fresh*
// one-member cluster on the empty dir and reports healthy. The Pod goes Ready,
// so no self-heal trigger can see it, while the client Service keeps routing a
// share of traffic to a member serving an empty keyspace.
//
// Corruption is not a substitute: corrupt files make etcd fail to boot, which
// is the crash-loop path TestPVCMemberCrashLoopSelfHeal already covers. The
// divergence is specifically empty-vs-corrupt, so this test wipes the dir.
//
// The wiped seed must therefore never come back serving an empty keyspace; it
// must fail to start and be replaced, with the pre-wipe data intact throughout.
func TestSeedDataDirLossDoesNotRebootstrap(t *testing.T) {
	ctx := context.Background()

	ns := &corev1.Namespace{
		TypeMeta:   metav1.TypeMeta{APIVersion: "v1", Kind: "Namespace"},
		ObjectMeta: metav1.ObjectMeta{Name: rebootstrapNamespace},
	}
	if err := kube.Patch(ctx, ns, client.Apply, fieldOwner, client.ForceOwnership); err != nil {
		t.Fatalf("create namespace %s: %v", rebootstrapNamespace, err)
	}
	t.Cleanup(func() {
		_ = kube.Delete(context.Background(), &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{Name: rebootstrapNamespace}})
	})

	three := int32(3)
	ec := &etcdv1alpha2.EtcdCluster{
		ObjectMeta: metav1.ObjectMeta{Name: rebootstrapCluster, Namespace: rebootstrapNamespace},
		Spec: etcdv1alpha2.EtcdClusterSpec{
			Replicas: &three,
			Version:  "3.6.11",
			Storage:  etcdv1alpha2.StorageSpec{Size: resource.MustParse("1Gi")},
		},
	}
	if err := kube.Create(ctx, ec); err != nil {
		t.Fatalf("create EtcdCluster: %v", err)
	}

	waitFor(ctx, t, 5*time.Minute, "cluster Available", etcdClusterAvailable(rebootstrapNamespace, rebootstrapCluster))
	waitFor(ctx, t, 2*time.Minute, "3 members ready", readyMembersIsIn(rebootstrapNamespace, rebootstrapCluster, 3))

	// Write through the client Service so the value is committed cluster-wide.
	// Its presence afterwards is what distinguishes "the cluster survived" from
	// "a member is answering out of a brand-new empty store".
	putSentinel(ctx, t)

	seed := seedMemberIn(ctx, t, rebootstrapNamespace, rebootstrapCluster)
	t.Logf("wiping data dir of seed member %q", seed)
	wipeMemberDataDir(ctx, t, seed)

	// Restart onto the now-empty volume. This is the moment
	// --initial-cluster-state is consulted.
	if err := kube.Delete(ctx, &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: seed, Namespace: rebootstrapNamespace}}); err != nil {
		t.Fatalf("delete seed pod %q: %v", seed, err)
	}

	waitFor(ctx, t, 15*time.Minute, fmt.Sprintf("wiped seed %q replaced rather than re-bootstrapped", seed),
		func(ctx context.Context) error {
			// Opportunistic fast-fail, not the primary assertion. If we catch
			// the wiped seed serving without the sentinel, it re-bootstrapped
			// and we can say so precisely instead of burning the full budget.
			// Missing it proves nothing: the surviving leader may already have
			// snapshotted the member back into consistency, which restores the
			// sentinel. The load-bearing assertion is the one below — a seed
			// that re-bootstrapped stays Ready and is therefore never replaced,
			// so this wait times out either way.
			//
			// It cannot produce a false failure: it fires only on a successful
			// read that returns something other than the value we wrote.
			if pod := readyPod(ctx, rebootstrapNamespace, seed); pod != nil {
				stdout, _, err := podExec(ctx, rebootstrapNamespace, seed, "etcd", []string{
					"etcdctl", "--endpoints=http://localhost:2379", "get", rebootstrapKey, "--print-value-only",
				})
				if err == nil && trimSpace(stdout) != rebootstrapValue {
					t.Fatalf("seed %q came up Ready serving an empty keyspace — it re-bootstrapped a fresh "+
						"one-member cluster on the wiped data dir instead of failing to start. "+
						"Clients reaching this member through the %s-client Service see no data. "+
						"(--initial-cluster-state must not be `new` once the cluster has formed)",
						seed, rebootstrapCluster)
				}
			}
			err := kube.Get(ctx, client.ObjectKey{Namespace: rebootstrapNamespace, Name: seed}, &etcdv1alpha2.EtcdMember{})
			if apierrors.IsNotFound(err) {
				return nil
			}
			if err != nil {
				return err
			}
			return fmt.Errorf("seed %q still present; it should be crash-looping on the empty data dir "+
				"and then replaced by self-heal", seed)
		})

	waitFor(ctx, t, 10*time.Minute, "cluster back to 3 ready members",
		readyMembersIsIn(rebootstrapNamespace, rebootstrapCluster, 3))

	// The decisive assertion: the data written before the wipe is still there.
	// A re-bootstrapped seed would have taken its share of client traffic into
	// an empty keyspace.
	assertSentinelIntact(ctx, t)
	t.Log("wiped seed failed to start, was replaced, and no data was lost")
}

// putSentinel writes the pre-wipe key via the cluster's client Service.
func putSentinel(ctx context.Context, t *testing.T) {
	t.Helper()
	pod := anyReadyMemberPod(ctx, t)
	endpoint := fmt.Sprintf("http://%s-client.%s.svc:2379", rebootstrapCluster, rebootstrapNamespace)
	if _, stderr, err := podExec(ctx, rebootstrapNamespace, pod, "etcd", []string{
		"etcdctl", "--endpoints=" + endpoint, "put", rebootstrapKey, rebootstrapValue,
	}); err != nil {
		t.Fatalf("etcdctl put sentinel: %v (stderr: %s)", err, stderr)
	}
}

// assertSentinelIntact reads the pre-wipe key back from every ready member, so
// a single member answering out of an empty store is caught rather than being
// averaged away by whichever endpoint the Service happened to pick.
func assertSentinelIntact(ctx context.Context, t *testing.T) {
	t.Helper()
	pods := &corev1.PodList{}
	if err := kube.List(ctx, pods, client.InNamespace(rebootstrapNamespace),
		client.MatchingLabels{"etcd-operator.cozystack.io/cluster": rebootstrapCluster}); err != nil {
		t.Fatalf("list member pods: %v", err)
	}
	checked := 0
	for i := range pods.Items {
		p := &pods.Items[i]
		if !podIsReady(p) {
			continue
		}
		stdout, stderr, err := podExec(ctx, rebootstrapNamespace, p.Name, "etcd", []string{
			"etcdctl", "--endpoints=http://localhost:2379", "get", rebootstrapKey, "--print-value-only",
		})
		if err != nil {
			t.Fatalf("etcdctl get on %s: %v (stderr: %s)", p.Name, err, stderr)
		}
		if got := trimSpace(stdout); got != rebootstrapValue {
			t.Fatalf("member %s serves %q for the pre-wipe key, want %q — data was lost", p.Name, got, rebootstrapValue)
		}
		checked++
	}
	if checked != 3 {
		t.Fatalf("expected to verify the sentinel on 3 ready members, checked %d", checked)
	}
}

// wipeMemberDataDir empties the member's data dir, leaving the volume mounted
// and writable — the "volume came back blank" shape (re-provisioned PV, blank
// restore, node-local storage lost on reimage), as opposed to the corrupt-files
// shape exercised elsewhere.
func wipeMemberDataDir(ctx context.Context, t *testing.T, member string) {
	t.Helper()
	pod, err := clientset.CoreV1().Pods(rebootstrapNamespace).Get(ctx, member, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get seed pod %q: %v", member, err)
	}
	pod.Spec.EphemeralContainers = append(pod.Spec.EphemeralContainers, corev1.EphemeralContainer{
		EphemeralContainerCommon: corev1.EphemeralContainerCommon{
			Name:  "wipe-data",
			Image: "busybox:1.36",
			// The emptiness test is the last statement on purpose: it is what
			// the container's exit code reports. A bare `ls -A` succeeds
			// whether or not the directory is empty, so it would mask a partial
			// wipe (say a leftover file this container's UID cannot remove) and
			// the test would then fail much later, with the confusing symptom
			// of a seed that starts fine, instead of failing here.
			// Note the `||` on the listing itself: an unreadable data dir makes
			// `ls` fail with empty stdout, which a bare emptiness test would
			// read as a successful wipe.
			Command: []string{"sh", "-c",
				"rm -rf /var/lib/etcd/* /var/lib/etcd/.[!.]* 2>/dev/null; sync; " +
					`left=$(ls -A /var/lib/etcd) || { echo "cannot read data dir after wipe" >&2; exit 1; }; ` +
					`if [ -n "$left" ]; then echo "wipe incomplete, still present: $left" >&2; exit 1; fi`},
			VolumeMounts: []corev1.VolumeMount{
				{Name: "data", MountPath: "/var/lib/etcd"},
			},
		},
	})
	if _, err := clientset.CoreV1().Pods(rebootstrapNamespace).UpdateEphemeralContainers(
		ctx, member, pod, metav1.UpdateOptions{}); err != nil {
		t.Fatalf("add wipe-data ephemeral container to %q: %v", member, err)
	}
	waitFor(ctx, t, 3*time.Minute, "data-dir wipe container finished", func(ctx context.Context) error {
		p, err := clientset.CoreV1().Pods(rebootstrapNamespace).Get(ctx, member, metav1.GetOptions{})
		if err != nil {
			return err
		}
		for _, cs := range p.Status.EphemeralContainerStatuses {
			if cs.Name != "wipe-data" {
				continue
			}
			if cs.State.Terminated != nil {
				if cs.State.Terminated.ExitCode != 0 {
					t.Fatalf("wipe-data exited %d (%s); its container log names what survived the wipe",
						cs.State.Terminated.ExitCode, cs.State.Terminated.Reason)
				}
				return nil
			}
			return fmt.Errorf("wipe-data not finished: %+v", cs.State)
		}
		return fmt.Errorf("wipe-data status not reported yet")
	})
}

// anyReadyMemberPod returns the name of a member Pod with a ready etcd
// container, failing the test if there is none.
func anyReadyMemberPod(ctx context.Context, t *testing.T) string {
	t.Helper()
	pods := &corev1.PodList{}
	if err := kube.List(ctx, pods, client.InNamespace(rebootstrapNamespace),
		client.MatchingLabels{"etcd-operator.cozystack.io/cluster": rebootstrapCluster}); err != nil {
		t.Fatalf("list member pods: %v", err)
	}
	for i := range pods.Items {
		if podIsReady(&pods.Items[i]) {
			return pods.Items[i].Name
		}
	}
	t.Fatalf("no ready etcd member pod to probe")
	return ""
}

// readyPod returns the named Pod when its etcd container is ready, else nil.
func readyPod(ctx context.Context, namespace, name string) *corev1.Pod {
	pod := &corev1.Pod{}
	if err := kube.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, pod); err != nil {
		return nil
	}
	if !podIsReady(pod) {
		return nil
	}
	return pod
}

func podIsReady(p *corev1.Pod) bool {
	if p.Status.Phase != corev1.PodRunning {
		return false
	}
	for _, cs := range p.Status.ContainerStatuses {
		if cs.Name == "etcd" && cs.Ready {
			return true
		}
	}
	return false
}
