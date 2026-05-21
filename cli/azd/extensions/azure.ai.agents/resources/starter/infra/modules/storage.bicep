targetScope = 'resourceGroup'

@description('Location for the storage account')
param location string = resourceGroup().location

@description('Tags for all resources')
param tags object = {}

@description('AI Services account name')
param foundryAccountName string

@description('AI project name')
param foundryProjectName string

@description('Managed identity principal ID of the AI project')
param projectPrincipalId string

@description('Developer principal ID')
param principalId string

@description('Developer principal type')
param principalType string

@description('Connection name for the Foundry Project')
param connectionName string

@description('Optional. Full ARM resource ID of an existing Storage account to reuse. Cross-RG / cross-sub safe.')
param existingStorageAccountResourceId string = ''

@description('When true (typically networkMode=byo-vnet-standard), set publicNetworkAccess=Disabled and allowSharedKeyAccess=false on the new storage account. Ignored for BYO existing accounts (the user owns network config).')
param disablePublicNetworkAccess bool = false

var resourceToken = uniqueString(subscription().id, resourceGroup().id, location)

var hasExisting = !empty(existingStorageAccountResourceId)
var existingParts = split(existingStorageAccountResourceId, '/')
var existingSubId = hasExisting ? existingParts[2] : subscription().subscriptionId
var existingRg = hasExisting ? existingParts[4] : resourceGroup().name
var existingName = hasExisting ? last(existingParts) : ''

resource newStorageAccount 'Microsoft.Storage/storageAccounts@2023-05-01' = if (!hasExisting) {
  name: 'st${resourceToken}'
  location: location
  tags: tags
  sku: { name: 'Standard_LRS' }
  kind: 'StorageV2'
  identity: { type: 'SystemAssigned' }
  properties: {
    supportsHttpsTrafficOnly: true
    allowBlobPublicAccess: false
    allowSharedKeyAccess: !disablePublicNetworkAccess
    minimumTlsVersion: 'TLS1_2'
    accessTier: 'Hot'
    publicNetworkAccess: disablePublicNetworkAccess ? 'Disabled' : 'Enabled'
    networkAcls: disablePublicNetworkAccess ? {
      defaultAction: 'Deny'
      bypass: 'AzureServices'
    } : null
  }
}

resource existingStorageAccount 'Microsoft.Storage/storageAccounts@2023-05-01' existing = if (hasExisting) {
  name: existingName
  scope: resourceGroup(existingSubId, existingRg)
}

#disable-next-line BCP318
var resolvedName = hasExisting ? existingName : newStorageAccount.name
#disable-next-line BCP318
var resolvedId = hasExisting ? existingStorageAccount.id : newStorageAccount.id
#disable-next-line BCP318
var resolvedLocation = hasExisting ? existingStorageAccount.location : newStorageAccount.location
#disable-next-line BCP318
var resolvedBlobEndpoint = hasExisting ? existingStorageAccount.properties.primaryEndpoints.blob : newStorageAccount.properties.primaryEndpoints.blob

// Pre-cap-host RBAC: Storage Blob Data Contributor on the storage account
// for the project MI and (optionally) the developer. Executed in the
// storage account's RG scope to support BYO cross-RG / cross-sub.
module storageRbac './storage-rbac.bicep' = {
  name: 'storage-rbac-${resolvedName}'
  scope: resourceGroup(existingSubId, existingRg)
  params: {
    storageAccountName: resolvedName
    projectPrincipalId: projectPrincipalId
    principalId: principalId
    principalType: principalType
  }
  dependsOn: [
    existingStorageAccount
  ]
}

// Connection to Foundry Project
module storageConnection './connection.bicep' = {
  name: 'storage-connection'
  params: {
    foundryAccountName: foundryAccountName
    foundryProjectName: foundryProjectName
    connectionConfig: {
      name: connectionName
      category: 'AzureStorageAccount'
      target: resolvedBlobEndpoint
      authType: 'AAD'
      isSharedToAll: true
      metadata: {
        ApiType: 'Azure'
        ResourceId: resolvedId
        location: resolvedLocation
      }
    }
  }
}

output accountName string = resolvedName
output accountId string = resolvedId
output accountSubscriptionId string = existingSubId
output accountResourceGroupName string = existingRg
output connectionName string = storageConnection.outputs.connectionName
