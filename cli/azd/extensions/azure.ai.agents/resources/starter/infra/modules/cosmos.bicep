targetScope = 'resourceGroup'

/*
  Azure Cosmos DB (SQL) account for Foundry Standard Setup BYO threadStorage.

  - Creates a new account (kind=GlobalDocumentDB, serverless capability)
    in the current RG, OR references an existing Cosmos DB account
    cross-RG / cross-subscription via existingCosmosDbAccountResourceId.
  - Creates a project connection (category=CosmosDB) on the Foundry project.

  RBAC for the Cosmos data plane is split into separate cosmos-rbac-pre and
  cosmos-rbac-post modules, applied at different points in main.bicep around
  the project capability host creation.
*/

@description('Location for the new Cosmos DB account (ignored for existing accounts).')
param location string = resourceGroup().location

@description('Tags for new resources (ignored for existing accounts).')
param tags object = {}

@description('Optional. Full ARM resource ID of an existing Cosmos DB account to reuse. Cross-RG / cross-sub safe.')
param existingCosmosDbAccountResourceId string = ''

@description('AI Foundry account name (used to wire the project connection).')
param aiAccountName string

@description('AI Foundry project name (used to wire the project connection).')
param aiProjectName string

@description('Project connection name to create on the Foundry project.')
param connectionName string

@description('When true (typically networkMode=byo-vnet-standard), set publicNetworkAccess=Disabled on the new account.')
param disablePublicNetworkAccess bool = false

var hasExisting = !empty(existingCosmosDbAccountResourceId)
var existingParts = split(existingCosmosDbAccountResourceId, '/')
var existingSubId = hasExisting ? existingParts[2] : subscription().subscriptionId
var existingRg = hasExisting ? existingParts[4] : resourceGroup().name
var existingName = hasExisting ? last(existingParts) : ''

var resourceToken = uniqueString(subscription().id, resourceGroup().id, location)
var newAccountName = 'cosmos-${resourceToken}'

resource newCosmos 'Microsoft.DocumentDB/databaseAccounts@2024-11-15' = if (!hasExisting) {
  name: newAccountName
  location: location
  tags: tags
  kind: 'GlobalDocumentDB'
  identity: { type: 'SystemAssigned' }
  properties: {
    databaseAccountOfferType: 'Standard'
    consistencyPolicy: {
      defaultConsistencyLevel: 'Session'
    }
    locations: [
      {
        locationName: location
        failoverPriority: 0
        isZoneRedundant: false
      }
    ]
    capabilities: [
      { name: 'EnableServerless' }
    ]
    publicNetworkAccess: disablePublicNetworkAccess ? 'Disabled' : 'Enabled'
    disableLocalAuth: false
  }
}

resource existingCosmos 'Microsoft.DocumentDB/databaseAccounts@2024-11-15' existing = if (hasExisting) {
  name: existingName
  scope: resourceGroup(existingSubId, existingRg)
}

#disable-next-line BCP318
var resolvedAccountName = hasExisting ? existingName : newCosmos.name
#disable-next-line BCP318
var resolvedAccountId = hasExisting ? existingCosmos.id : newCosmos.id
#disable-next-line BCP318
var resolvedEndpoint = hasExisting ? existingCosmos.properties.documentEndpoint : newCosmos.properties.documentEndpoint

// Project connection (via shared connection.bicep)
module cosmosConnection './connection.bicep' = {
  name: 'cosmos-connection-${resolvedAccountName}'
  params: {
    aiAccountName: aiAccountName
    aiProjectName: aiProjectName
    connectionConfig: {
      name: connectionName
      category: 'CosmosDB'
      target: resolvedEndpoint
      authType: 'AAD'
      isSharedToAll: true
      metadata: {
        ApiType: 'Azure'
        ResourceId: resolvedAccountId
        location: location
      }
    }
  }
}

output accountName string = resolvedAccountName
output accountId string = resolvedAccountId
output endpoint string = resolvedEndpoint
output accountSubscriptionId string = existingSubId
output accountResourceGroupName string = existingRg
output connectionName string = cosmosConnection.outputs.connectionName
