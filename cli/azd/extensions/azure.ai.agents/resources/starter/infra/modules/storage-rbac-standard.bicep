targetScope = 'resourceGroup'

/*
  Post-cap-host Storage RBAC for the project managed identity.

  Assigns the built-in Storage Blob Data Owner role with a condition scoping
  it to the workspace-owned agent containers
  (`<workspaceId>*-azureml-agent`). Must run AFTER the project capability
  host is created because the cap host provisions the agent containers.

  Mirrors the foundry-samples 15-private-network-standard-agent-setup pattern.
*/

@description('Name of the storage account in the current RG scope.')
param storageAccountName string

@description('Project managed identity principal ID.')
param projectPrincipalId string

@description('Project workspace GUID derived from project.properties.internalId.')
param workspaceId string

resource storage 'Microsoft.Storage/storageAccounts@2023-05-01' existing = {
  name: storageAccountName
}

// Built-in: Storage Blob Data Owner
var blobDataOwnerRoleId = 'b7e6dc6d-f1e8-4753-8033-0f276bb0955b'

var conditionStr = '((!(ActionMatches{\'Microsoft.Storage/storageAccounts/blobServices/containers/blobs/tags/read\'})  AND  !(ActionMatches{\'Microsoft.Storage/storageAccounts/blobServices/containers/blobs/filter/action\'}) AND  !(ActionMatches{\'Microsoft.Storage/storageAccounts/blobServices/containers/blobs/tags/write\'}) ) OR (@Resource[Microsoft.Storage/storageAccounts/blobServices/containers:name] StringStartsWithIgnoreCase \'${workspaceId}\' AND @Resource[Microsoft.Storage/storageAccounts/blobServices/containers:name] StringLikeIgnoreCase \'*-azureml-agent\'))'

resource blobDataOwnerAssignment 'Microsoft.Authorization/roleAssignments@2022-04-01' = {
  scope: storage
  name: guid(storage.id, projectPrincipalId, blobDataOwnerRoleId, workspaceId)
  properties: {
    principalId: projectPrincipalId
    principalType: 'ServicePrincipal'
    roleDefinitionId: subscriptionResourceId('Microsoft.Authorization/roleDefinitions', blobDataOwnerRoleId)
    conditionVersion: '2.0'
    condition: conditionStr
  }
}
