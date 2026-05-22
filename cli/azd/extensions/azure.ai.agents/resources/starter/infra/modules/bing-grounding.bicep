targetScope = 'resourceGroup'

@description('Tags for all resources')
param tags object = {}

@description('AI Services account name')
param foundryAccountName string

@description('AI project name')
param foundryProjectName string

@description('Managed identity principal ID of the AI project')
param projectPrincipalId string

@description('Connection name for the Foundry Project')
param connectionName string

@description('Optional. Full ARM resource ID of an existing Microsoft.Bing/accounts (Grounding) account to reuse. Cross-RG / cross-sub safe. The caller must have listKeys permission on the existing account (the connection is created with the account key).')
param existingBingGroundingResourceId string = ''

var resourceToken = uniqueString(subscription().id, resourceGroup().id)

var hasExisting = !empty(existingBingGroundingResourceId)
var existingParts = split(existingBingGroundingResourceId, '/')
var existingSubId = hasExisting ? existingParts[2] : subscription().subscriptionId
var existingRg = hasExisting ? existingParts[4] : resourceGroup().name
var existingName = hasExisting ? last(existingParts) : ''

resource newBingSearch 'Microsoft.Bing/accounts@2020-06-10' = if (!hasExisting) {
  name: 'bing-${resourceToken}'
  location: 'global'
  tags: tags
  sku: { name: 'G1' }
  kind: 'Bing.Grounding'
  properties: { statisticsEnabled: false }
}

resource existingBingSearch 'Microsoft.Bing/accounts@2020-06-10' existing = if (hasExisting) {
  name: existingName
  scope: resourceGroup(existingSubId, existingRg)
}

#disable-next-line BCP318
var resolvedName = hasExisting ? existingName : newBingSearch.name
#disable-next-line BCP318
var resolvedId = hasExisting ? existingBingSearch.id : newBingSearch.id
#disable-next-line BCP318
var resolvedEndpoint = hasExisting ? existingBingSearch.properties.endpoint : newBingSearch.properties.endpoint
// BCP422: Bicep can't prove which branch of `existing if` is taken statically,
// so calling listKeys() on either conditional resource produces a warning. The
// ternary picks exactly one branch at runtime; both calls are safe under the
// `hasExisting` gate. The connection module requires a non-empty key value
// (ApiKey auth), so a BYO Bing account requires the deployer to have listKeys
// permission on the existing account.
#disable-next-line BCP318 BCP422
var resolvedKey = hasExisting ? existingBingSearch.listKeys().key1 : newBingSearch.listKeys().key1

// Project MI: Cognitive Services User. Only assigned when we create the account.
// For BYO existing accounts the user owns RBAC on their account.
#disable-next-line BCP318
resource bingRole 'Microsoft.Authorization/roleAssignments@2022-04-01' = if (!hasExisting) {
  scope: newBingSearch
  name: guid(newBingSearch.id, projectPrincipalId, 'a97b65f3-24c7-4388-baec-2e87135dc908')
  properties: {
    principalId: projectPrincipalId
    principalType: 'ServicePrincipal'
    roleDefinitionId: resourceId('Microsoft.Authorization/roleDefinitions', 'a97b65f3-24c7-4388-baec-2e87135dc908')
  }
}

// Connection to Foundry Project
module bingConnection './connection.bicep' = {
  name: 'bing-connection'
  params: {
    foundryAccountName: foundryAccountName
    foundryProjectName: foundryProjectName
    connectionConfig: {
      name: connectionName
      category: 'GroundingWithBingSearch'
      target: resolvedEndpoint
      authType: 'ApiKey'
      isSharedToAll: true
      metadata: {
        Location: 'global'
        ResourceId: resolvedId
        ApiType: 'Azure'
        type: 'bing_grounding'
      }
    }
    credentials: { key: resolvedKey }
  }
  dependsOn: [bingRole]
}

output bingName string = resolvedName
output bingId string = resolvedId
output connectionName string = bingConnection.outputs.connectionName
output connectionId string = bingConnection.outputs.connectionId
