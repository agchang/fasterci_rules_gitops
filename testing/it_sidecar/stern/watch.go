//   Copyright 2016 Wercker Holding BV
//
//   Licensed under the Apache License, Version 2.0 (the "License");
//   you may not use this file except in compliance with the License.
//   You may obtain a copy of the License at
//
//       http://www.apache.org/licenses/LICENSE-2.0
//
//   Unless required by applicable law or agreed to in writing, software
//   distributed under the License is distributed on an "AS IS" BASIS,
//   WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//   See the License for the specific language governing permissions and
//   limitations under the License.

package stern

import (
	"context"
	"fmt"
	"log"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/watch"
	v1 "k8s.io/client-go/kubernetes/typed/core/v1"
)

// Target is a target to watch
type Target struct {
	Namespace string
	Pod       string
	Container string
}

// GetID returns the ID of the object
func (t *Target) GetID() string {
	return fmt.Sprintf("%s-%s-%s", t.Namespace, t.Pod, t.Container)
}

// Watch starts listening to Kubernetes events and emits modified
// containers/pods. The first result is targets added, the second is targets
// removed
func Watch(ctx context.Context, i v1.PodInterface, containerState ContainerState, labelSelector labels.Selector, allowErrors bool, onAdded, onRemoved func(*Target)) error {
	watcher, err := i.Watch(ctx, metav1.ListOptions{Watch: true, LabelSelector: labelSelector.String()})
	if err != nil {
		return fmt.Errorf("failed to set up watch: %s", err)
	}

	defer watcher.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case e := <-watcher.ResultChan():
			if e.Object == nil {
				// watcher channel was closed (because of error)
				return nil
			}

			var (
				pod *corev1.Pod
				ok  bool
			)
			if pod, ok = e.Object.(*corev1.Pod); !ok {
				continue
			}

			log.Printf("pod %s/%s event %s", pod.Namespace, pod.Name, e.Type)

			switch e.Type {
			case watch.Added, watch.Modified:
				var statuses []corev1.ContainerStatus
				statuses = append(statuses, pod.Status.InitContainerStatuses...)
				statuses = append(statuses, pod.Status.ContainerStatuses...)

				for _, c := range statuses {
					// if c.RestartCount > 0 {
					// 	log.Print("container ", c.Name, " has restart count ", c.RestartCount)
					// 	return
					// }

					log.Print("container ", c.Name, " has state ", c.State)

					if !allowErrors {
						if t := c.State.Terminated; t != nil && t.ExitCode != 0 && t.Reason == "Error" {
							return fmt.Errorf("container %s failed with exit code %d and reason '%s'", c.Name, t.ExitCode, t.Reason)
						}
					}

					if containerState.Match(c.State) {
						onAdded(&Target{
							Namespace: pod.Namespace,
							Pod:       pod.Name,
							Container: c.Name,
						})
					}
				}
			case watch.Deleted:
				var containers []corev1.Container
				containers = append(containers, pod.Spec.Containers...)
				containers = append(containers, pod.Spec.InitContainers...)

				for _, c := range containers {

					onRemoved(&Target{
						Namespace: pod.Namespace,
						Pod:       pod.Name,
						Container: c.Name,
					})
				}
			}
		}
	}

}
