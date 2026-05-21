targetScope = 'resourceGroup'

/*
  Project-scoped capability host (sample 15 pattern) for byo-vnet-standard.

  Must be created AFTER all of:
    - account-level capability host (caphostacct)
    - project itself
    - project connections to Cosmos, Storage, and AI Search
    - pre-cap-host RBAC (Cosmos Operator, Storage Blob Data Contributor,
      Search Service Contributor + Index Data Contributor)
    - private endpoints + DNS zones for the data resources

  Post-cap-host RBAC modules (cosmos-rbac-post, storage-rbac-standard) MUST run
  AFTER this module.
*/

@description('AI Foundry account name in the current RG.')
param foundryAccountName string

@description('AI Foundry project name (under the account).')
param foundryProjectName string

@description('Project connection name for the AI Search service (vectorStoreConnections).')
param foundrySearchConnectionName string

@description('Project connection name for the Storage account (storageConnections).')
param storageConnectionName string

@description('Project connection name for the Cosmos DB account (threadStorageConnections).')
param cosmosDbConnectionName string

resource foundryAccount 'Microsoft.CognitiveServices/accounts@2025-06-01' existing = {
  name: foundryAccountName

  resource project 'projects' existing = {
    name: foundryProjectName
  }
}

resource projectCapHost 'Microsoft.CognitiveServices/accounts/projects/capabilityHosts@2025-04-01-preview' = {
  parent: foundryAccount::project
  name: 'caphostproj'
  properties: {
    #disable-next-line BCP037
    capabilityHostKind: 'Agents'
    #disable-next-line BCP037
    vectorStoreConnections: [foundrySearchConnectionName]
    #disable-next-line BCP037
    storageConnections: [storageConnectionName]
    #disable-next-line BCP037
    threadStorageConnections: [cosmosDbConnectionName]
  }
}

output projectCapHostName string = projectCapHost.name
