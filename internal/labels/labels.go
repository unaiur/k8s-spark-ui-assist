// Package labels centralises the Kubernetes label keys and values that identify
// Spark driver pods.  Both the watcher and the API package must agree on these
// values; keeping them in one place prevents silent drift.
package labels

const (
	// LabelInstance is the label key used to identify the Spark job instance.
	LabelInstance = "app.kubernetes.io/instance"
	// LabelRole is the label key used to distinguish driver from executor pods.
	LabelRole = "spark-role"
	// LabelSelector is the label key carrying the Spark application selector ID.
	LabelSelector = "spark-app-selector"
	// LabelAppName is the label key carrying the Spark application name.
	LabelAppName = "spark-app-name"

	// InstanceValue is the expected value of LabelInstance for Spark jobs managed
	// by this operator.
	InstanceValue = "spark-job"
	// RoleValue is the expected value of LabelRole for driver pods.
	RoleValue = "driver"
)

// DriverSelector returns the label selector string that matches all Spark driver
// pods managed by this operator.
func DriverSelector() string {
	return LabelInstance + "=" + InstanceValue + "," + LabelRole + "=" + RoleValue
}

// DriverSelectorForApp returns the label selector string that matches the driver
// pod for a specific Spark application identified by appSelector.
func DriverSelectorForApp(appSelector string) string {
	return DriverSelector() + "," + LabelSelector + "=" + appSelector
}
