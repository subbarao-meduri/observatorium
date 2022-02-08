# Multiple Metrics Write Endpoints Proposal

* **Owners:**:
  * `@marcolan018`

* **Related Tickets:**
  * `<JIRA, GH Issues>`

* **Other docs:**
  * [Exporting Metrics To Additional Consumers in RedHat Advanced Cluster Management for Kubernetes](https://docs.google.com/document/d/1dItpwlI9IoYYLyIR1Vl9cCSf0rF8L-Fc4C0wpNA4wKw/edit#heading=h.ju4q7sud5qdb) (internal Red Hat link)

## TLDR

We propose implementing an API that supports multiple metrics write backends. The metrics write requests can be forwarded to more than one backend endpoints.

## Why

The proposed API provides the feature that exports the metrics data to external endpoints except the thanos receivers when the client posts the metrics data to the API.

## Goals

* Observatorium API can run with more than one "metrics.write.endpoint" arguments, and metrics data will be forwarded to all of the configured endpoints

## Non-Goals

* Other types of observability data(e.g. logs) to multiple endpoints not considered
* Write endpoints which require security check not considered

## Action Plan

* Iterate and finalise this design document.
* Implement the Observatorium API to forward metrics to multiple endpoints.
* Update Observatorium operator to support multiple write endpoints.
