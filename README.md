# node-upgrader

This controller is designed to assist in upgrading Kubernetes clusters by being to cordon and drain nodes by applying a instance of the NodeDrain Custom Resource Definition to the cluster.

The controller will validate that the node specified by `nodeName` is of the same verision as the one specified by `expectedK8sVersion` before it makes any changes as an extra check.

The CRD can be used to just Ccrden a node by setting `drain` to `false`

If using this controller to preform kubrnetes upgrades it is advisable to move this controller onto the new nodes before peforming draining of nodes the workflow for this might be create NodeDrain crds for all nodes with drain `false',  delete the node-drain controller pod causing it to be moved to one of the new nodes then you can switch `drain` to `true` to drain nodes as needed

**Note: This ccontroller is to be considred alpha software and is not advisable to use in production without testing first**

## The NodeDrain CRD
```
apiVersion: gezb.io/v1
kind: NodeDrain
metadata:
  name: nodedrain-sample
spec:
  active: true                  # If set to false will run in dry-run mode and just log what the NodeDrain would do 
  nodeName: node01              # The name of the node to be cordoned and if specified drained
  expectedK8sVersion: v1.19.16  # The expected version of the node to be acted on in the form vMAJOR.MINOR.PATCH  (ignoring prerelease and metadata)
  drain: true                   # If this node should be drained after being cordoned
```

## Getting Started

You’ll need a Kubernetes cluster to run against. You can use [KIND](https://sigs.k8s.io/kind) to get a local cluster for testing, or run against a remote cluster.
**Note:** Your controller will automatically use the current context in your kubeconfig file (i.e. whatever cluster `kubectl cluster-info` shows).

### Running on the cluster
1. Install Instances of Custom Resources:

```sh
make install
```to

2. Build and push your image to the location specified by `IMG`:
	
```sh
make docker-build docker-push IMG=<some-registry>/node-upgrader:tag
```
	
3. Deploy the controller to the cluster with the image specified by `IMG`:

```sh
make deploy IMG=<some-registry>/node-upgrader:tag
```

### Uninstall CRDs
To delete the CRDs from the cluster:

```sh
make uninstall
```

### Undeploy controller
UnDeploy the controller to the cluster:

```sh
make undeploy
```

### How it works
This project aims to follow the Kubernetes [Operator pattern](https://kubernetes.io/docs/concepts/extend-kubernetes/operator/)

It uses [Controllers](https://kubernetes.io/docs/concepts/architecture/controller/) 
which provides a reconcile function responsible for synchronizing resources until the desired state is reached on the cluster 

### Test It Out
1. Install the CRDs into the cluster:

```sh
make install
```

2. Run your controller (this will run in the foreground, so switch to a new terminal if you want to leave it running):

```sh
make run
```

**NOTE:** You can also run this in one step by running: `make install run`

### Modifying the API definitions
If you are editing the API definitions, generate the manifests such as CRs or CRDs using:

```sh
make manifests
```

**NOTE:** Run `make --help` for more information on all potential `make` targets

More information can be found via the [Kubebuilder Documentation](https://book.kubebuilder.io/introduction.html)

## License

Copyright 2022.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.

