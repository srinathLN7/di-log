**create-service-per-pod.jsonnet**

This script appears to be a Lua function that is used as a hook in Metacontroller. It's executed when a StatefulSet resource is created or updated. Here's a step-by-step breakdown:

1. **Function Declaration:**
   ```lua
   function(request) {
   ```
   This declares a function that takes a single argument `request`.

2. **Local Variables:**
   ```lua
   local statefulset = request.object,
   local labelKey = statefulset.metadata.annotations["service-per-pod-label"],
   local ports = statefulset.metadata.annotations["service-per-pod-ports"],
   ```
   - `statefulset`: This variable holds the StatefulSet object extracted from the `request` argument.
   - `labelKey`: It extracts the value of the annotation `"service-per-pod-label"` from the StatefulSet's metadata.
   - `ports`: It extracts the value of the annotation `"service-per-pod-ports"` from the StatefulSet's metadata.

3. **Attachments:**
   ```lua
   attachments: [
     {
       ...
     }
     for index in std.range(0, statefulset.spec.replicas - 1)
   ]
   ```
   This section generates a list of Service objects based on the StatefulSet's configuration. It iterates over a range of numbers from 0 to `statefulset.spec.replicas - 1` (number of replicas minus one) and creates a Service object for each replica.

4. **Service Object Creation:**
   - It creates a Service object for each replica, setting the name based on the StatefulSet's name and the replica's index.
   - Labels the Service with `app: "service-per-pod"`.
   - Sets the Service type as `"LoadBalancer"`.
   - Defines the selector to match the `labelKey` with the Service's name and index.
   - Parses the `ports` annotation, which appears to be a comma-separated list of port definitions, and creates corresponding Service ports.

5. **Function Closing:**
   ```lua
   }
   ```
   Closes the function definition.

**delete-service-per-pod.jsonnet**

This script is another Lua function used as a hook in Metacontroller. It's executed when a StatefulSet is finalized, meaning when it's deleted or removed. Here's the breakdown:

1. **Function Declaration:**
   ```lua
   function(request) {  
   ```
   Declares a function that takes a single argument `request`.

2. **Attachments and Finalization:**
   ```lua
   attachments: [],  
   finalized: std.length(request.attachments['Service.v1']) == 0
   ```
   - `attachments`: An empty list of attachments is provided.
   - `finalized`: This checks if the length of the list of attachments of type 'Service.v1' (presumably Service resources) is equal to zero. If there are no attached Service resources, it considers the finalization process to be complete.

3. **Function Closing:**
   ```lua
   }
   ```
   Closes the function definition.

In summary, File 1 is a Lua script used to dynamically create Service resources based on the annotations and configuration of a StatefulSet. File 2 is a Lua script used to determine if a StatefulSet has been finalized (deleted or removed) by checking the absence of attached Service resources. These scripts are part of Metacontroller's customization capabilities for handling Kubernetes resources.
