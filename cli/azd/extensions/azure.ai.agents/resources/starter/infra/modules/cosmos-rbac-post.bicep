targetScope = 'resourceGroup'

/*
  Post-cap-host Cosmos DB data-plane RBAC for the project managed identity.

  Assigns the built-in Cosmos DB Built-in Data Contributor role
  (00000000-0000-0000-0000-000000000002) on /dbs/enterprise_memory using
  sqlRoleAssignments. Must run AFTER the project capability host is created
  because the cap host provisions the 'enterprise_memory' database.

  Mirrors the foundry-samples 15-private-network-standard-agent-setup pattern.
*/

@description('Name of the Cosmos DB account in the current RG scope.')
param cosmosAccountName string

@description('Project managed identity principal ID.')
param projectPrincipalId string

@description('Project workspace GUID derived from project.properties.internalId (used to make the role assignment name deterministic).')
param projectWorkspaceId string

resource cosmosAccount 'Microsoft.DocumentDB/databaseAccounts@2024-11-15' existing = {
  name: cosmosAccountName
}

// Built-in data-plane role definition ID (Cosmos DB Built-in Data Contributor)
var dataContributorRoleId = '00000000-0000-0000-0000-000000000002'

var roleDefinitionResourceId = resourceId(
  'Microsoft.DocumentDB/databaseAccounts/sqlRoleDefinitions',
  cosmosAccountName,
  dataContributorRoleId
)

var enterpriseMemoryScope = '${cosmosAccount.id}/dbs/enterprise_memory'

resource dataContributorAssignment 'Microsoft.DocumentDB/databaseAccounts/sqlRoleAssignments@2024-11-15' = {
  parent: cosmosAccount
  name: guid(projectWorkspaceId, cosmosAccountName, dataContributorRoleId, projectPrincipalId)
  properties: {
    principalId: projectPrincipalId
    roleDefinitionId: roleDefinitionResourceId
    scope: enterpriseMemoryScope
  }
}
