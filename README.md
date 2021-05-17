# Triggermesh Function

Function CRD is the experimental Triggermesh component that provides a way to
deploy [KLR](https://github.com/triggermesh/knative-lambda-runtime) functions
without the requirement to build Docker images. At the moment, Function
controller can run [Python](config/samples/python.yaml),
[Node JS](config/samples/nodejs.yaml) and [Ruby](config/samples/ruby.yaml) code.

## Specification

Function object specification is straightforward:

- `runtime` - the name of the language runtime to use for this function, can be
  `python`, `node` or `ruby`

- `public` - boolean value when set to `true` makes the function endpoint
  accessible from the Internet. The default value is `false`
  
- `ceOverrides` - cloudevents overrides

- `entrypoint` - the name of the function in the code to use as an entrypoint

- `code` - inline source code of the Function

## Installation

Function CRD can be compiled and deployed from source with
[ko](https://github.com/google/ko):

```
ko apply -f ./config
```

You can verify that it's installed by checking that the controller is running:

```
$ kubectl -n function get pods -l app=function-controller
NAME                                 READY   STATUS    RESTARTS   AGE
function-controller-7446cc55bd-frchl   1/1     Running   0          3h26m
```

A custom resource of kind `Function` can now be created, check
[samples](config/samples/) directory.

## Support

We would love your feedback and help on these sources, so don't hesitate to let
us know what is wrong and how we could improve them, just file an
[issue](https://github.com/triggermesh/function/issues/new) or join those of use
who are maintaining them and submit a
[PR](https://github.com/triggermesh/function/compare)

## Commercial Support

TriggerMesh Inc supports this project commercially, email info@triggermesh.com
to get more details.

## Code of Conduct

This plugin is by no means part of [CNCF](https://www.cncf.io/) but we abide by
its
[code of conduct](https://github.com/cncf/foundation/blob/master/code-of-conduct.md)

