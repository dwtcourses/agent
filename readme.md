### Data format

GRPC is used for calls between agent and integrations. Endpoints and parameters are defined using .proto files.

Integrations are responsible for getting the data and converting it to pinpoint format. Integrations will use datamodel directly. Agent itself does not need to check the datamodel. Agent will use the metadata to correctly forward that data to backend, but does not have to touch the data itself.

### Export code flow

When agent export command is called, agent loads all available/configured plugins and then inits them using the Init call to allow them to call back to the agent.

After that agent calls Export methods on integrations in parallel. Integration marks the export state for each model type using ExportStarted and ExportDone. It sends the data using SendExported call.

### Agent RPC interface

Name | Desc
--- | ---
ExportStarted | Called after starting export for a certain type.
ExportDone | Called after export is completed for a certain type. 
SendExported | Forwards the exported objects from intergration to agent, which then uploads the data (or queues for uploading).
ExportGitRepo | Integration can ask agent to download and process git repo using ripsrc.

TODO: which call should handle the last processed timestamp

### Integration RPC interface

Name | Desc
--- | ---
Init | Provides the connection details for connecting back to agent
Export | Starts the export of all data types for this integration