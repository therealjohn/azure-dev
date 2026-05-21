targetScope = 'resourceGroup'

@description('Location for the container registry')
param location string = resourceGroup().location

@description('Tags for all resources')
param tags object = {}

@description('AI Services account name (for creating the project connection)')
param foundryAccountName string

@description('AI project name (for creating the project connection)')
param foundryProjectName string

@description('Managed identity principal ID of the AI project (for AcrPull role)')
param projectPrincipalId string

@description('Developer principal ID (for Container Registry Tasks Contributor role)')
param principalId string

@description('Developer principal type')
param principalType string

@description('Whether the deployment is reusing an existing Foundry project. When true, role assignments on the ACR are skipped: the existing project MI is assumed to already have AcrPull (the ACR name is deterministic per (sub, rg, location), so prior deployments may have created the assignment under a different guid()), and the developer is assumed to already have the Container Registry Tasks Contributor role. This avoids RoleAssignmentExists conflicts on re-deploys against existing projects.')
param useExistingFoundryProject bool = false

var resourceToken = uniqueString(subscription().id, resourceGroup().id, location)
var registryName = 'cr${resourceToken}'
var connectionName = 'acr-${resourceToken}'

// Role assignments are skipped for existing projects to avoid RoleAssignmentExists
// conflicts on re-deploys. The ACR name is deterministic per (sub, rg, location),
// so prior deployments may have created the assignments under a different guid().
var acrRoleAssignments = useExistingFoundryProject ? [] : [
  {
    // Container Registry Tasks Contributor -- developer: build & push images via ACR Tasks
    principalId: principalId
    principalType: principalType
    roleDefinitionIdOrName: 'fb382eab-e894-4461-af04-94435c366c3f'
  }
  {
    // AcrPull -- project managed identity: pull images for hosted agents
    principalId: projectPrincipalId
    principalType: 'ServicePrincipal'
    roleDefinitionIdOrName: '7f951dda-4ed3-4680-a7ca-43fe172d538d'
  }
]

module containerRegistry 'br/public:avm/res/container-registry/registry:0.1.1' = {
  name: 'acr-${registryName}'
  params: {
    name: registryName
    location: location
    tags: tags
    acrSku: 'Basic'
    acrAdminUserEnabled: false
    roleAssignments: acrRoleAssignments
  }
}

// Connection to Foundry Project
module acrConnection './connection.bicep' = {
  name: 'acr-connection'
  params: {
    foundryAccountName: foundryAccountName
    foundryProjectName: foundryProjectName
    connectionConfig: {
      name: connectionName
      category: 'ContainerRegistry'
      target: containerRegistry.outputs.loginServer
      authType: 'ManagedIdentity'
      isSharedToAll: true
      metadata: { ResourceId: containerRegistry.outputs.resourceId }
    }
    credentials: {
      clientId: projectPrincipalId
      resourceId: containerRegistry.outputs.resourceId
    }
  }
}

output registryName string = containerRegistry.outputs.name
output loginServer string = containerRegistry.outputs.loginServer
output connectionName string = acrConnection.outputs.connectionName
