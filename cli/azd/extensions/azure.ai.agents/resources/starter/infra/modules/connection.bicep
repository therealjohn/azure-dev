targetScope = 'resourceGroup'

@description('AI Services account name')
param foundryAccountName string

@description('AI project name')
param foundryProjectName string

@description('Connection configuration object')
param connectionConfig object

@secure()
@description('Credentials for the connection (e.g. { key: "..." } for ApiKey)')
param credentials object = {}

resource foundryAccount 'Microsoft.CognitiveServices/accounts@2025-04-01-preview' existing = {
  name: foundryAccountName

  resource project 'projects' existing = {
    name: foundryProjectName
  }
}

resource connection 'Microsoft.CognitiveServices/accounts/projects/connections@2025-04-01-preview' = {
  parent: foundryAccount::project
  name: connectionConfig.name
  properties: {
    category: connectionConfig.category
    target: connectionConfig.target
    authType: connectionConfig.authType
    isSharedToAll: connectionConfig.?isSharedToAll ?? true
    credentials: !empty(credentials) ? credentials : null
    metadata: connectionConfig.?metadata
  }
}

output connectionName string = connection.name
output connectionId string = connection.id
