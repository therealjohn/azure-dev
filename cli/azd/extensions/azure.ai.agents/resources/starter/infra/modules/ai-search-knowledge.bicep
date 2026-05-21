targetScope = 'resourceGroup'

@description('Name of the existing storage account that backs the search indexer.')
param storageAccountName string

@description('Name of the existing AI Search service whose managed identity is granted Storage Blob Data Reader.')
param searchServiceName string

@description('Name of the blob container created for the search indexer.')
param knowledgeContainerName string = 'knowledge'

resource storageAccount 'Microsoft.Storage/storageAccounts@2023-05-01' existing = {
  name: storageAccountName
}

resource blobService 'Microsoft.Storage/storageAccounts/blobServices@2023-05-01' existing = {
  parent: storageAccount
  name: 'default'
}

resource searchService 'Microsoft.Search/searchServices@2024-06-01-preview' existing = {
  name: searchServiceName
}

resource knowledgeContainer 'Microsoft.Storage/storageAccounts/blobServices/containers@2023-05-01' = {
  parent: blobService
  name: knowledgeContainerName
  properties: { publicAccess: 'None' }
}

// Search MI -> Storage: Blob Data Reader
resource searchToStorageRole 'Microsoft.Authorization/roleAssignments@2022-04-01' = {
  scope: storageAccount
  name: guid(storageAccount.id, searchService.id, '2a2b9908-6ea1-4ae2-8e65-a410df84e7d1')
  properties: {
    principalId: searchService.identity.principalId
    principalType: 'ServicePrincipal'
    roleDefinitionId: subscriptionResourceId('Microsoft.Authorization/roleDefinitions', '2a2b9908-6ea1-4ae2-8e65-a410df84e7d1')
  }
}

output knowledgeContainerName string = knowledgeContainer.name
