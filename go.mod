module github.com/aws/aws-node-termination-handler

go 1.16

require (
	github.com/Masterminds/sprig/v3 v3.2.2
	github.com/aws/aws-sdk-go v1.33.1
	github.com/golang/groupcache v0.0.0-20210331224755-41bb18bfe9da // indirect
	github.com/huandu/xstrings v1.3.2 // indirect
	github.com/mitchellh/copystructure v1.2.0 // indirect
	github.com/rs/zerolog v1.18.0
	go.opentelemetry.io/contrib/instrumentation/runtime v0.6.1
	go.opentelemetry.io/otel v0.6.0
	go.opentelemetry.io/otel/exporters/metric/prometheus v0.6.0
	golang.org/x/sys v0.0.0-20200302150141-5c8b2ff67527
	golang.org/x/time v0.0.0-20190921001708-c4c64cad1fd0 // indirect
	k8s.io/api v0.0.0-20191010143144-fbf594f18f80
	k8s.io/apimachinery v0.0.0-20191016060620-86f2f1b9c076
	k8s.io/client-go v0.0.0-20191014070654-bd505ee787b2
	k8s.io/kubectl v0.0.0-20191016234702-5d0b8f240400
)
