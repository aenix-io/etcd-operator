/*
Copyright 2024 The etcd-operator Authors.

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

package factory

import (
	"slices"
	"time"

	etcdaenixiov1alpha1 "github.com/aenix-io/etcd-operator/api/v1alpha1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ = Describe("Condition builder", func() {
	Context("When updating condition", func() {
		etcdCluster := &etcdaenixiov1alpha1.EtcdCluster{}

		It("should fill conditions", func() {
			FillConditions(etcdCluster)
			Expect(etcdCluster.Status.Conditions).NotTo(BeEmpty())
			for _, c := range etcdCluster.Status.Conditions {
				Expect(c.Status).To(Equal(metav1.ConditionFalse))
				Expect(c.LastTransitionTime).NotTo(BeZero())
			}
		})

		It("should update existing condition", func() {
			conditionsLength := len(etcdCluster.Status.Conditions)
			SetCondition(etcdCluster, NewCondition(etcdaenixiov1alpha1.EtcdConditionInitialized).
				WithStatus(false).
				WithReason(string(etcdaenixiov1alpha1.EtcdCondTypeInitStarted)).
				WithMessage("test").
				Complete())
			Expect(len(etcdCluster.Status.Conditions)).To(Equal(conditionsLength))
		})

		It("should keep last transition timestamp without status change, otherwise update", func() {
			idx := slices.IndexFunc(etcdCluster.Status.Conditions, func(condition metav1.Condition) bool {
				return condition.Type == etcdaenixiov1alpha1.EtcdConditionInitialized
			})
			timestamp := etcdCluster.Status.Conditions[idx].LastTransitionTime

			By("setting condition without status change")
			SetCondition(etcdCluster, NewCondition(etcdaenixiov1alpha1.EtcdConditionInitialized).
				WithStatus(false).
				WithReason(string(etcdaenixiov1alpha1.EtcdCondTypeInitStarted)).
				WithMessage("test").
				Complete())
			Expect(etcdCluster.Status.Conditions[idx].LastTransitionTime).To(Equal(timestamp))

			By("setting condition with status changed")
			SetCondition(etcdCluster, NewCondition(etcdaenixiov1alpha1.EtcdConditionInitialized).
				WithStatus(true).
				WithReason(string(etcdaenixiov1alpha1.EtcdCondTypeInitStarted)).
				WithMessage("test").
				Complete())
			Expect(etcdCluster.Status.Conditions[idx].LastTransitionTime).NotTo(Equal(timestamp))
		})
	})

	Context("when retrieving conditions", func() {
		It("should return nil if condition of such type is not present", func() {
			etcdCluster := &etcdaenixiov1alpha1.EtcdCluster{
				Status: etcdaenixiov1alpha1.EtcdClusterStatus{
					Conditions: []metav1.Condition{
						{
							Type:               etcdaenixiov1alpha1.EtcdConditionInitialized,
							Status:             metav1.ConditionTrue,
							ObservedGeneration: 0,
							LastTransitionTime: metav1.NewTime(time.Now()),
							Reason:             string(etcdaenixiov1alpha1.EtcdCondTypeInitComplete),
							Message:            string(etcdaenixiov1alpha1.EtcdInitCondPosMessage),
						},
					},
				},
			}

			Expect(GetCondition(etcdCluster, etcdaenixiov1alpha1.EtcdConditionReady)).To(BeNil())
		})

		It("should return correct condition from the list", func() {
			expectedCond := metav1.Condition{
				Type:               etcdaenixiov1alpha1.EtcdConditionReady,
				Status:             metav1.ConditionTrue,
				ObservedGeneration: 0,
				LastTransitionTime: metav1.NewTime(time.Now()),
				Reason:             string(etcdaenixiov1alpha1.EtcdCondTypeStatefulSetReady),
				Message:            string(etcdaenixiov1alpha1.EtcdReadyCondPosMessage),
			}

			etcdCluster := &etcdaenixiov1alpha1.EtcdCluster{
				Status: etcdaenixiov1alpha1.EtcdClusterStatus{
					Conditions: []metav1.Condition{
						expectedCond,
						{
							Type:               etcdaenixiov1alpha1.EtcdConditionInitialized,
							Status:             metav1.ConditionTrue,
							ObservedGeneration: 0,
							LastTransitionTime: metav1.NewTime(time.Now()),
							Reason:             string(etcdaenixiov1alpha1.EtcdCondTypeInitComplete),
							Message:            string(etcdaenixiov1alpha1.EtcdInitCondPosMessage),
						},
					},
				},
			}
			foundCond := GetCondition(etcdCluster, etcdaenixiov1alpha1.EtcdConditionReady)
			if Expect(foundCond).NotTo(BeNil()) {
				Expect(*foundCond).To(Equal(expectedCond))
			}
		})
	})
})
