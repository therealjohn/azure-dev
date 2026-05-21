targetScope = 'resourceGroup'

/*
  Pre-cap-host Cosmos DB RBAC for the project managed identity.

  Assigns Cosmos DB Operator (control-plane) so the Foundry control plane can
  bind the project's MI to the Cosmos account before the project capability
  host is created. Must run BEFORE the project cap host.

  Module is scoped to the Cosmos account's resource group; pass the right
  scope from main.bicep when the Cosmos account lives in a different RG.
*/

@description('Name of the Cosmos DB account in the current RG scope.')
param cosmosAccountName string

@description('Project managed identity principal ID.')
param projectPrincipalId string

resource cosmosAccount 'Microsoft.DocumentDB/databaseAccounts@2024-11-15' existing = {
  name: cosmosAccountName
}

// Built-in role: Cosmos DB Operator
var cosmosOperatorRoleId = '230815da-be43-4aae-9cb4-875f7bd000aa'

resource cosmosOperatorRoleAssignment 'Microsoft.Authorization/roleAssignments@2022-04-01' = {
  scope: cosmosAccount
  name: guid(cosmosAccount.id, projectPrincipalId, cosmosOperatorRoleId)
  properties: {
    principalId: projectPrincipalId
    principalType: 'ServicePrincipal'
    roleDefinitionId: subscriptionResourceId('Microsoft.Authorization/roleDefinitions', cosmosOperatorRoleId)
  }
}
