targetScope = 'resourceGroup'

@description('Location for all resources')
param location string = resourceGroup().location

@description('Tags for all resources')
param tags object = {}

@description('AI Services account name')
param aiAccountName string

@description('AI project name')
param aiProjectName string

@description('Managed identity principal ID of the AI project')
param projectPrincipalId string

@description('Developer principal ID')
param principalId string

@description('Developer principal type')
param principalType string

@description('Connection name for the Foundry Project')
param connectionName string

@description('Optional. Full ARM resource ID of an existing AI Search service to reuse. Cross-RG / cross-sub safe.')
param existingSearchServiceResourceId string = ''

@description('When true (typically networkMode=byo-vnet-standard), set publicNetworkAccess=disabled on the new search service. Ignored for BYO existing services (the user owns network config).')
param disablePublicNetworkAccess bool = false

var resourceToken = uniqueString(subscription().id, resourceGroup().id, location)

var hasExisting = !empty(existingSearchServiceResourceId)
var existingParts = split(existingSearchServiceResourceId, '/')
var existingSubId = hasExisting ? existingParts[2] : subscription().subscriptionId
var existingRg = hasExisting ? existingParts[4] : resourceGroup().name
var existingName = hasExisting ? last(existingParts) : ''

resource newSearchService 'Microsoft.Search/searchServices@2024-06-01-preview' = if (!hasExisting) {
  name: 'search-${resourceToken}'
  location: location
  tags: tags
  sku: { name: 'basic' }
  identity: { type: 'SystemAssigned' }
  properties: {
    replicaCount: 1
    partitionCount: 1
    hostingMode: 'default'
    authOptions: {
      aadOrApiKey: { aadAuthFailureMode: 'http401WithBearerChallenge' }
    }
    publicNetworkAccess: disablePublicNetworkAccess ? 'disabled' : 'enabled'
  }
}

resource existingSearchService 'Microsoft.Search/searchServices@2024-06-01-preview' existing = if (hasExisting) {
  name: existingName
  scope: resourceGroup(existingSubId, existingRg)
}

#disable-next-line BCP318
var resolvedName = hasExisting ? existingName : newSearchService.name
#disable-next-line BCP318
var resolvedId = hasExisting ? existingSearchService.id : newSearchService.id

// RBAC: split into a sub-module so we can scope to the existing search RG / sub.
module searchRbac './ai-search-rbac.bicep' = {
  name: 'ai-search-rbac-${resolvedName}'
  scope: resourceGroup(existingSubId, existingRg)
  params: {
    searchServiceName: resolvedName
    aiAccountName: aiAccountName
    projectPrincipalId: projectPrincipalId
    principalId: principalId
    principalType: principalType
  }
  dependsOn: [
    existingSearchService
  ]
}

// Connection to Foundry Project
module searchConnection './connection.bicep' = {
  name: 'search-connection'
  params: {
    aiAccountName: aiAccountName
    aiProjectName: aiProjectName
    connectionConfig: {
      name: connectionName
      category: 'CognitiveSearch'
      target: 'https://${resolvedName}.search.windows.net'
      authType: 'AAD'
      isSharedToAll: true
      metadata: {
        ApiVersion: '2024-07-01'
        ResourceId: resolvedId
        ApiType: 'Azure'
        type: 'azure_ai_search'
      }
    }
  }
  dependsOn: [searchRbac]
}

output serviceName string = resolvedName
output serviceId string = resolvedId
output serviceSubscriptionId string = existingSubId
output serviceResourceGroupName string = existingRg
output connectionName string = searchConnection.outputs.connectionName
