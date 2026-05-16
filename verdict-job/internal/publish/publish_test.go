package publish

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/dlh/dlh-test-fw/verdict-job/internal/eval"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestPublishCreatesConfigMap(t *testing.T) {
	cs := fake.NewSimpleClientset()
	p := &Publisher{Cs: cs, Namespace: "dlh-test-fw"}
	r := &eval.Result{Overall: true, ChaosVerdict: "Pass"}
	if err := p.Publish(context.Background(), "wf-xyz", r); err != nil {
		t.Fatal(err)
	}
	cm, err := cs.CoreV1().ConfigMaps("dlh-test-fw").Get(context.Background(), "dlh-result-wf-xyz", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(cm.Data["result.json"]), &got); err != nil {
		t.Fatal(err)
	}
	if got["overall"] != true {
		t.Errorf("overall=%v", got["overall"])
	}
}

func TestPublishUpdatesExistingConfigMap(t *testing.T) {
	cs := fake.NewSimpleClientset()
	p := &Publisher{Cs: cs, Namespace: "dlh-test-fw"}
	_ = p.Publish(context.Background(), "wf-xyz", &eval.Result{Overall: false, ChaosVerdict: "Fail"})
	if err := p.Publish(context.Background(), "wf-xyz", &eval.Result{Overall: true, ChaosVerdict: "Pass"}); err != nil {
		t.Fatal(err)
	}
	cm, _ := cs.CoreV1().ConfigMaps("dlh-test-fw").Get(context.Background(), "dlh-result-wf-xyz", metav1.GetOptions{})
	var got map[string]any
	_ = json.Unmarshal([]byte(cm.Data["result.json"]), &got)
	if got["overall"] != true {
		t.Errorf("update didn't take: %v", got)
	}
}
