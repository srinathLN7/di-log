
This `README.md` explains the configuration of the Metacontroller components specified in the file `service-per-pod.yaml` in your Kubernetes cluster.

## DecoratorController

The DecoratorController is responsible for managing specific resources in your cluster based on the presence of annotations in StatefulSets.

```yaml
apiVersion: metacontroller.k8s.io/v1alpha1
kind: DecoratorController
metadata:
  name: service-per-pod
spec:
  resources:
  - apiVersion: apps/v1
    resource: statefulsets
    annotationSelector:
      matchExpressions:
      - {key: service-per-pod-label, operator: Exists}
      - {key: service-per-pod-ports, operator: Exists}
  attachments:
  - apiVersion: v1
    resource: services
  hooks:
    sync:
      webhook:
        url: "http://service-per-pod.metacontroller/create-service-per-pod"
    finalize:
      webhook:
        url: "http://service-per-pod.metacontroller/delete-service-per-pod"
```

The DecoratorController watches StatefulSets with specific annotations and attaches to Services. It also defines synchronization and finalization webhooks.

## ConfigMap

A ConfigMap named `service-per-pod-hooks` is used for storing configuration data.

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  namespace: metacontroller
  name: service-per-pod-hooks
data:
{{ (.Files.Glob "hooks/*").AsConfig | indent 2 }}
```

The ConfigMap is populated with data from files matching the pattern `"hooks/*"`.

## Deployment

A Deployment named `service-per-pod` is responsible for running containers within pods.

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: service-per-pod
  namespace: metacontroller
spec:
  replicas: 1
  selector:
    matchLabels:
      app: service-per-pod
  template:
    metadata:
      labels:
        app: service-per-pod
  containers:
  - name: hooks
    image: metacontroller/jsonnetd:0.1
    imagePullPolicy: Always
    workingDir: /hooks
    volumeMounts:
    - name: hooks
      mountPath: /hooks
  volumes:
  - name: hooks
    configMap:
      name: service-per-pod-hooks
```

This Deployment runs a container (`hooks`) using the specified Docker image, mounting a volume from the `service-per-pod-hooks` ConfigMap.

## Service

A Service named `service-per-pod` is created to expose the pods managed by the Deployment.

```yaml
apiVersion: v1
kind: Service
metadata:
  name: service-per-pod
  namespace: metacontroller
spec:
  selector:
    app: service-per-pod
  ports:
  - port: 80
    targetPort: 8080
```

This Service selects pods with the label `app: service-per-pod` and forwards incoming traffic on port 80 to port 8080 on the selected pods.

## Conditional Inclusion

The entire configuration is conditionally included based on the value of `.Values.service.lb`:

```yaml
{{ if .Values.service.lb }}
...
{{ end }}
```

If `.Values.service.lb` is evaluated as true, the configuration will be applied; otherwise, it will be excluded.
```

Feel free to use and modify this `README.md` file as part of your documentation for your Metacontroller configuration.


This YAML configuration appears to be part of a Kubernetes configuration file for a DecoratorController, ConfigMap, Deployment, and Service in a Kubernetes cluster. Let's break it down line by line:

```yaml
{{ if .Values.service.lb }}
```

- This line appears to be using a templating mechanism. It checks if the value of `.Values.service.lb` evaluates to true. Depending on the value of `.Values.service.lb`, the subsequent YAML block will be included or excluded.

```yaml
apiVersion: metacontroller.k8s.io/v1alpha1
kind: DecoratorController
metadata:
  name: service-per-pod
```

- This defines a Kubernetes resource of kind `DecoratorController` named `service-per-pod`. DecoratorControllers are used in the context of Metacontroller, an extended webhook-based API server for Kubernetes.

```yaml
spec:
  resources:
  - apiVersion: apps/v1
    resource: statefulsets
    annotationSelector:
      matchExpressions:
      - {key: service-per-pod-label, operator: Exists}
      - {key: service-per-pod-ports, operator: Exists}
```

- This specifies the resources that this DecoratorController will act upon. In this case, it's specifying that it will apply to StatefulSets with specific annotations. It appears to filter StatefulSets based on the presence of certain annotations.

```yaml
attachments:
- apiVersion: v1
  resource: services
```

- This section specifies what resources this DecoratorController will attach to. It indicates that this controller will attach to Services.

```yaml
hooks:
  sync:
    webhook:
      url: "http://service-per-pod.metacontroller/create-service-per-pod"
  finalize:
    webhook:
      url: "http://service-per-pod.metacontroller/delete-service-per-pod"
```

- These hooks define actions to take during the synchronization and finalization phases. They specify URLs of webhooks that will be triggered at specific points in the lifecycle of this DecoratorController.

```yaml
---
apiVersion: v1
kind: ConfigMap
metadata:
  namespace: metacontroller
  name: service-per-pod-hooks
data:
{{ (.Files.Glob "hooks/*").AsConfig | indent 2 }}
```

- This defines a ConfigMap named `service-per-pod-hooks` in the `metacontroller` namespace. It appears to use templating (`{{ ... }}`) to include the content of files matching the pattern `"hooks/*"` from some source (possibly ConfigMap data).

```yaml
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: service-per-pod
  namespace: metacontroller
```

- This section defines a Deployment named `service-per-pod` in the `metacontroller` namespace. Deployments are used to manage the lifecycle of pods.

```yaml
spec:
  replicas: 1
  selector:
    matchLabels:
      app: service-per-pod
  template:
    metadata:
      labels:
        app: service-per-pod
```

- These lines specify the Deployment's desired state. It includes one replica, selects pods with the label `app: service-per-pod`, and specifies a pod template with labels.

```yaml
  containers:
  - name: hooks
    image: metacontroller/jsonnetd:0.1
    imagePullPolicy: Always
    workingDir: /hooks
    volumeMounts:
    - name: hooks
      mountPath: /hooks
  volumes:
  - name: hooks
    configMap:
      name: service-per-pod-hooks
```

- This section specifies the containers that will run within the pods managed by this Deployment. It defines a single container named `hooks` using the `metacontroller/jsonnetd:0.1` Docker image. It also mounts a volume named `hooks` from the ConfigMap `service-per-pod-hooks` into the `/hooks` directory within the container.

```yaml
---
apiVersion: v1
kind: Service
metadata:
  name: service-per-pod
  namespace: metacontroller
spec:
  selector:
    app: service-per-pod
  ports:
  - port: 80
    targetPort: 8080
```

- This defines a Kubernetes Service named `service-per-pod` in the `metacontroller` namespace. The Service selects pods with the label `app: service-per-pod` and exposes port 80, forwarding traffic to port 8080 on the selected pods.

```yaml
{{ end }}
```

- This line closes the conditional check that started at the beginning of the configuration. If `.Values.service.lb` was evaluated as false, this part of the YAML would be excluded from the final configuration.

In summary, this configuration file defines a DecoratorController that operates on StatefulSets with specific annotations and attaches to Services. It also includes ConfigMaps, Deployments, and Services, along with the necessary settings for these resources. The use of templating and conditional statements allows certain parts of the configuration to be included or excluded based on the value of `.Values.service.lb`. This configuration seems to be part of a larger setup or application within a Kubernetes cluster.
