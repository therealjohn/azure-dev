targetScope = 'resourceGroup'

@description('AI Search service name (in the current RG scope).')
param searchServiceName string

@description('AI Foundry account name (target of the search-to-account RBAC).')
param aiAccountName string

@description('Project managed identity principal ID.')
param projectPrincipalId string

@description('Developer principal ID.')
param principalId string = ''

@description('Developer principal type.')
param principalType string = 'User'

resource searchService 'Microsoft.Search/searchServices@2024-06-01-preview' existing = {
  name: searchServiceName
}

// Search -> AI Services: Cognitive Services OpenAI User
resource searchToAiRole 'Microsoft.Authorization/roleAssignments@2022-04-01' = {
  name: guid(aiAccountName, searchService.id, '5e0bd9bd-7b93-4f28-af87-19fc36ad61bd')
  properties: {
    principalId: searchService.identity.principalId
    principalType: 'ServicePrincipal'
    roleDefinitionId: subscriptionResourceId('Microsoft.Authorization/roleDefinitions', '5e0bd9bd-7b93-4f28-af87-19fc36ad61bd')
  }
}

// Project MI -> Search: Search Service Contributor
resource projectToSearchContributorRole 'Microsoft.Authorization/roleAssignments@2022-04-01' = {
  scope: searchService
  name: guid(searchService.id, projectPrincipalId, '7ca78c08-252a-4471-8644-bb5ff32d4ba0')
  properties: {
    principalId: projectPrincipalId
    principalType: 'ServicePrincipal'
    roleDefinitionId: subscriptionResourceId('Microsoft.Authorization/roleDefinitions', '7ca78c08-252a-4471-8644-bb5ff32d4ba0')
  }
}

// Project MI -> Search: Search Index Data Contributor
resource projectToSearchDataRole 'Microsoft.Authorization/roleAssignments@2022-04-01' = {
  scope: searchService
  name: guid(searchService.id, projectPrincipalId, '8ebe5a00-799e-43f5-93ac-243d3dce84a7')
  properties: {
    principalId: projectPrincipalId
    principalType: 'ServicePrincipal'
    roleDefinitionId: subscriptionResourceId('Microsoft.Authorization/roleDefinitions', '8ebe5a00-799e-43f5-93ac-243d3dce84a7')
  }
}

// Developer -> Search: Search Index Data Contributor
resource userToSearchRole 'Microsoft.Authorization/roleAssignments@2022-04-01' = if (!empty(principalId)) {
  scope: searchService
  name: guid(searchService.id, principalId, '8ebe5a00-799e-43f5-93ac-243d3dce84a7')
  properties: {
    principalId: principalId
    principalType: principalType
    roleDefinitionId: subscriptionResourceId('Microsoft.Authorization/roleDefinitions', '8ebe5a00-799e-43f5-93ac-243d3dce84a7')
  }
}
