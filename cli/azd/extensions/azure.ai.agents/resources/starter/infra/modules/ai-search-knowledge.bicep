targetScope = 'resourceGroup'

@description('Name of the existing storage account that backs the search indexer. Must live in the resource group this module is deployed into (call this module with scope: resourceGroup(storageSub, storageRg)).')
param storageAccountName string

@description('Subscription ID where the search service lives. Defaults to the deployment subscription. Pass the value from ai-search.bicep `serviceSubscriptionId` output to support BYO cross-sub search.')
param searchServiceSubscriptionId string = subscription().subscriptionId

@description('Resource group name where the search service lives. Defaults to the deployment resource group. Pass the value from ai-search.bicep `serviceResourceGroupName` output to support BYO cross-RG search.')
param searchServiceResourceGroupName string = resourceGroup().name

@description('Name of the existing AI Search service whose managed identity is granted Storage Blob Data Reader on the storage account in this module\'s scope.')
param searchServiceName string

@description('Name of the blob container created for the search indexer.')
param knowledgeContainerName string = 'knowledge'

resource storageAccount 'Microsoft.Storage/storageAccounts@2025-08-01' existing = {
  name: storageAccountName
}

resource blobService 'Microsoft.Storage/storageAccounts/blobServices@2025-08-01' existing = {
  parent: storageAccount
  name: 'default'
}

resource searchService 'Microsoft.Search/searchServices@2024-06-01-preview' existing = {
  name: searchServiceName
  scope: resourceGroup(searchServiceSubscriptionId, searchServiceResourceGroupName)
}

resource knowledgeContainer 'Microsoft.Storage/storageAccounts/blobServices/containers@2025-08-01' = {
  parent: blobService
  name: knowledgeContainerName
  properties: { publicAccess: 'None' }
}

// Search MI -> Storage: Blob Data Reader. Created in the storage account's RG
// scope (this module's scope), so it works for BYO cross-RG / cross-sub storage.
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
