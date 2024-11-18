/*
Copyright 2024 The Uyuni Project.

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

package controller

import (
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	ctrl "sigs.k8s.io/controller-runtime"

	uyuniv1alpha1 "github.com/cbosdo/uyuni-server-operator/api/v1alpha1"
	adm_kubernetes "github.com/uyuni-project/uyuni-tools/mgradm/shared/kubernetes"
	"github.com/uyuni-project/uyuni-tools/shared/kubernetes"
	"github.com/uyuni-project/uyuni-tools/shared/utils"
)

func (r *ServerReconciler) serverDeployment(server *uyuniv1alpha1.Server) (*appsv1.Deployment, error) {
	// TODO Share code with uyuni-tools!
	envs := []corev1.EnvVar{
		{Name: "TZ", Value: server.Spec.Timezone},
	}

	volumes, mounts := volumesForServer(server)

	// Mount the volumes in /mnt for the initialization.
	initMounts := []corev1.VolumeMount{}
	for _, mount := range mounts {
		initMount := mount.DeepCopy()
		initMount.MountPath = "/mnt" + initMount.MountPath
		initMounts = append(initMounts, *initMount)
	}

	if server.Spec.Volumes.Mirror != "" {
		// Add the environment variable for the deployment to use the mirror
		// This doesn't makes sense for migration as the setup script is not executed
		envs = append(envs, corev1.EnvVar{Name: "MIRROR_PATH", Value: mirrorMountPath})
	}

	runMount, runVolume := kubernetes.CreateTmpfsMount("/run", "256Mi")
	cgroupMount, cgroupVolume := kubernetes.CreateHostPathMount(
		"/sys/fs/cgroup", "/sys/fs/cgroup", corev1.HostPathDirectory,
	)

	caMount := corev1.VolumeMount{
		Name:      "ca-cert",
		MountPath: "/etc/pki/trust/anchors/LOCAL-RHN-ORG-TRUSTED-SSL-CERT",
		ReadOnly:  true,
		SubPath:   "ca.crt",
	}
	tlsKeyMount := corev1.VolumeMount{Name: "tls-key", MountPath: "/etc/pki/spacewalk-tls"}

	caVolume := kubernetes.CreateSecretVolume("ca-cert", server.Spec.SSL.ServerSecretName)

	tlsKeyVolume := kubernetes.CreateSecretVolume("tls-key", server.Spec.SSL.ServerSecretName)
	var keyMode int32 = 0600
	tlsKeyVolume.VolumeSource.Secret.Items = []corev1.KeyToPath{
		{Key: "tls.crt", Path: "spacewalk.crt"},
		{Key: "tls.key", Path: "spacewalk.key", Mode: &keyMode},
	}

	initMounts = append(initMounts, tlsKeyMount)
	mounts = append(mounts, runMount, cgroupMount, caMount, tlsKeyMount)
	volumes = append(volumes, runVolume, cgroupVolume, caVolume, tlsKeyVolume)

	ports := utils.GetServerPorts(server.Spec.Debug)

	labels := labelsForServer()
	var replicas int32 = 1

	dep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      adm_kubernetes.ServerDeployName,
			Namespace: server.Namespace,
			Labels:    labels,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Strategy: appsv1.DeploymentStrategy{Type: appsv1.RecreateDeploymentStrategyType},
			Selector: &metav1.LabelSelector{
				MatchLabels: labels,
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: labels},
				Spec: corev1.PodSpec{
					InitContainers: []corev1.Container{
						{
							Name:         "init-volumes",
							Image:        server.Spec.Image,
							Command:      []string{"sh", "-x", "-c", initScript},
							VolumeMounts: initMounts,
						},
					},
					Containers: []corev1.Container{
						{
							Name:  "uyuni",
							Image: server.Spec.Image,
							Lifecycle: &corev1.Lifecycle{
								PreStop: &corev1.LifecycleHandler{
									Exec: &corev1.ExecAction{
										Command: []string{"/bin/sh", "-c", "spacewalk-service stop && systemctl stop postgresql"},
									},
								},
							},
							Ports: kubernetes.ConvertPortMaps(ports),
							Env:   envs,
							ReadinessProbe: &corev1.Probe{
								ProbeHandler: corev1.ProbeHandler{
									HTTPGet: &corev1.HTTPGetAction{
										Port: intstr.FromInt(80),
										Path: "/rhn/metrics",
									},
								},
								PeriodSeconds:    30,
								TimeoutSeconds:   20,
								FailureThreshold: 5,
							},
							LivenessProbe: &corev1.Probe{
								ProbeHandler: corev1.ProbeHandler{
									HTTPGet: &corev1.HTTPGetAction{
										Port: intstr.FromInt(80),
										Path: "/rhn/metrics",
									},
								},
								InitialDelaySeconds: 60,
								PeriodSeconds:       60,
								TimeoutSeconds:      20,
								FailureThreshold:    5,
							},
							VolumeMounts: mounts,
						},
					},
					Volumes: volumes,
				},
			},
		},
	}

	pullPolicy := getPullPolicy(server.Spec.PullPolicy)
	if pullPolicy != nil {
		dep.Spec.Template.Spec.Containers[0].ImagePullPolicy = *pullPolicy
		dep.Spec.Template.Spec.InitContainers[0].ImagePullPolicy = *pullPolicy
	}

	if server.Spec.PullSecret != "" {
		dep.Spec.Template.Spec.ImagePullSecrets = []corev1.LocalObjectReference{
			{Name: server.Spec.PullSecret},
		}
	}

	// Set the ownerRef for the Deployment
	// More info: https://kubernetes.io/docs/concepts/overview/working-with-objects/owners-dependents/
	if err := ctrl.SetControllerReference(server, dep, r.Scheme); err != nil {
		return nil, err
	}
	return dep, nil
}

const initScript = `
# Fill he empty volumes
for vol in /var/lib/cobbler \
		   /var/lib/salt \
		   /var/lib/pgsql \
		   /var/cache \
		   /var/log \
		   /srv/salt \
		   /srv/www \
		   /srv/tftpboot \
		   /srv/formula_metadata \
		   /srv/pillar \
		   /srv/susemanager \
		   /srv/spacewalk \
		   /root \
		   /etc/apache2 \
		   /etc/rhn \
		   /etc/systemd/system/multi-user.target.wants \
		   /etc/systemd/system/sockets.target.wants \
		   /etc/salt \
		   /etc/tomcat \
		   /etc/cobbler \
		   /etc/sysconfig \
		   /etc/postfix \
		   /etc/sssd \
		   /etc/pki/tls
do
	chown --reference=$vol /mnt$vol;
	chmod --reference=$vol /mnt$vol;
	if [ -z "$(ls -A /mnt$vol)" ]; then
    	cp -a $vol/. /mnt$vol;
		if [ "$vol" = "/srv/www" ]; then
            ln -s /etc/pki/trust/anchors/LOCAL-RHN-ORG-TRUSTED-SSL-CERT /mnt$vol/RHN-ORG-TRUSTED-SSL-CERT;
		fi

		if [ "$vol" = "/etc/pki/tls" ]; then
              ln -s /etc/pki/spacewalk-tls/spacewalk.crt /mnt/etc/pki/tls/certs/spacewalk.crt;
              ln -s /etc/pki/spacewalk-tls/spacewalk.key /mnt/etc/pki/tls/private/spacewalk.key;
              cp /etc/pki/spacewalk-tls/spacewalk.key /mnt/etc/pki/tls/private/pg-spacewalk.key;
              chown postgres:postgres /mnt/etc/pki/tls/private/pg-spacewalk.key;
		fi
	fi
done
`

const mirrorMountPath = "/mirror"

func volumesForServer(server *uyuniv1alpha1.Server) ([]corev1.Volume, []corev1.VolumeMount) {
	mounts := []corev1.VolumeMount{
		{MountPath: "/var/lib/cobbler", Name: "var-cobbler"},
		{MountPath: "/var/lib/salt", Name: "var-salt"},
		{MountPath: "/var/cache", Name: "var-cache"},
		{MountPath: "/var/spacewalk", Name: "var-spacewalk"},
		{MountPath: "/var/log", Name: "var-log"},
		{MountPath: "/srv/salt", Name: "srv-salt"},
		{MountPath: "/srv/www/", Name: "srv-www"},
		{MountPath: "/srv/tftpboot", Name: "srv-tftpboot"},
		{MountPath: "/srv/formula_metadata", Name: "srv-formulametadata"},
		{MountPath: "/srv/pillar", Name: "srv-pillar"},
		{MountPath: "/srv/susemanager", Name: "srv-susemanager"},
		{MountPath: "/srv/spacewalk", Name: "srv-spacewalk"},
		{MountPath: "/root", Name: "root"},
		{MountPath: "/etc/apache2", Name: "etc-apache2"},
		{MountPath: "/etc/systemd/system/multi-user.target.wants", Name: "etc-systemd-multi"},
		{MountPath: "/etc/systemd/system/sockets.target.wants", Name: "etc-systemd-sockets"},
		{MountPath: "/etc/salt", Name: "etc-salt"},
		{MountPath: "/etc/tomcat", Name: "etc-tomcat"},
		{MountPath: "/etc/cobbler", Name: "etc-cobbler"},
		{MountPath: "/etc/sysconfig", Name: "etc-sysconfig"},
		{MountPath: "/etc/postfix", Name: "etc-postfix"},
		{MountPath: "/etc/sssd", Name: "etc-sssd"},
		{MountPath: "/etc/pki/tls", Name: "etc-tls"},
		{MountPath: "/var/lib/pgsql", Name: "var-pgsql"},
		{MountPath: "/etc/rhn", Name: "etc-rhn"},
	}

	if server.Spec.Volumes.Mirror != "" {
		mounts = append(mounts, corev1.VolumeMount{Name: server.Spec.Volumes.Mirror, MountPath: mirrorMountPath})
	}

	volumes := []corev1.Volume{}
	for _, mount := range mounts {
		volumes = append(volumes, corev1.Volume{
			Name: mount.Name,
			VolumeSource: corev1.VolumeSource{
				PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
					ClaimName: mount.Name,
				},
			},
		})
	}

	return volumes, mounts
}

func getVolume(name string, pvcName string) corev1.Volume {
	return corev1.Volume{
		Name: name,
		VolumeSource: corev1.VolumeSource{
			PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
				ClaimName: pvcName,
			},
		},
	}
}
