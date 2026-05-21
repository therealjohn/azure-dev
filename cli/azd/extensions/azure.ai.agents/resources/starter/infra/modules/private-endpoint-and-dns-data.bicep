targetScope = 'resourceGroup'

/*
  Private endpoints + DNS zones for the BYO data resources used by the Standard
  network mode: Azure Cosmos DB (SQL), Azure Storage (blob), Azure AI Search.

  Each resource gets:
    - a PE on the supplied PE subnet
    - a private DNS zone (or an existing one referenced via existingDnsZones)
    - VNet link
    - DNS zone group on the PE

  This module is INVOKED ONCE PER DATA RESOURCE (use module loops in main.bicep
  with different params, or pass all three names together as below).
*/

@description('Name of the VNet that hosts the PE subnet.')
param vnetName string

@description('Location for the private endpoints. Must match the region of the subnet/VNet they are attached to. Defaults to the current resource group location.')
param location string = resourceGroup().location

@description('Subscription ID of the VNet (defaults to current).')
param vnetSubscriptionId string = subscription().subscriptionId

@description('Resource group of the VNet (defaults to current).')
param vnetResourceGroupName string = resourceGroup().name

@description('Name of the PE subnet on the VNet.')
param peSubnetName string

@description('Short unique suffix for generated names.')
param suffix string

@description('Cosmos DB account name (in the current RG scope or referenced via cosmosDbId).')
param cosmosDbName string

@description('Full ARM resource ID of the Cosmos DB account.')
param cosmosDbId string

@description('Storage account name (in the current RG scope or referenced via storageAccountId).')
param storageAccountName string

@description('Full ARM resource ID of the Storage account.')
param storageAccountId string

@description('AI Search service name (in the current RG scope or referenced via searchServiceId).')
param searchServiceName string

@description('Full ARM resource ID of the AI Search service.')
param searchServiceId string

@description('Map of zone FQDN -> resource group of an existing zone to reuse. Empty string means create new in the current resource group. When an existing zone is reused, the caller is responsible for ensuring it is linked to the spoke VNet (typical hub-spoke pattern); this module only creates VNet links for zones it creates.')
param existingDnsZones object = {
  'privatelink.documents.azure.com': ''
  'privatelink.search.windows.net': ''
}

@description('Subscription ID where existing private DNS zones live. Accepts bare GUID or /subscriptions/<guid> path.')
param dnsZonesSubscriptionId string = subscription().subscriptionId

var dnsZonesSubIsPath = startsWith(toLower(dnsZonesSubscriptionId), '/subscriptions/')
var resolvedDnsZonesSubscriptionId = dnsZonesSubIsPath ? split(dnsZonesSubscriptionId, '/')[2] : dnsZonesSubscriptionId

// Use environment().suffixes.storage for sovereign-cloud compatibility.
var blobZoneName = 'privatelink.blob.${environment().suffixes.storage}'
var cosmosZoneName = 'privatelink.documents.azure.com'
var searchZoneName = 'privatelink.search.windows.net'

var blobZoneRg = existingDnsZones[?blobZoneName] ?? ''
var cosmosZoneRg = existingDnsZones[?cosmosZoneName] ?? ''
var searchZoneRg = existingDnsZones[?searchZoneName] ?? ''

resource vnet 'Microsoft.Network/virtualNetworks@2024-05-01' existing = {
  name: vnetName
  scope: resourceGroup(vnetSubscriptionId, vnetResourceGroupName)
}

resource peSubnet 'Microsoft.Network/virtualNetworks/subnets@2024-05-01' existing = {
  parent: vnet
  name: peSubnetName
}

// ──── Cosmos DB ──────────────────────────────────────────────────────
resource cosmosPe 'Microsoft.Network/privateEndpoints@2024-05-01' = {
  name: '${cosmosDbName}-pe-${suffix}'
  location: location
  properties: {
    subnet: { id: peSubnet.id }
    privateLinkServiceConnections: [
      {
        name: '${cosmosDbName}-pls-${suffix}'
        properties: {
          privateLinkServiceId: cosmosDbId
          groupIds: ['Sql']
        }
      }
    ]
  }
}

resource newCosmosZone 'Microsoft.Network/privateDnsZones@2020-06-01' = if (empty(cosmosZoneRg)) {
  name: cosmosZoneName
  location: 'global'
}
resource existingCosmosZone 'Microsoft.Network/privateDnsZones@2020-06-01' existing = if (!empty(cosmosZoneRg)) {
  name: cosmosZoneName
  scope: resourceGroup(resolvedDnsZonesSubscriptionId, cosmosZoneRg)
}
#disable-next-line BCP318
var cosmosZoneId = empty(cosmosZoneRg) ? newCosmosZone.id : existingCosmosZone.id

resource newCosmosLink 'Microsoft.Network/privateDnsZones/virtualNetworkLinks@2024-06-01' = if (empty(cosmosZoneRg)) {
  parent: newCosmosZone
  location: 'global'
  name: 'cosmos-${suffix}-link'
  properties: {
    virtualNetwork: { id: vnet.id }
    registrationEnabled: false
  }
}

resource cosmosDnsGroup 'Microsoft.Network/privateEndpoints/privateDnsZoneGroups@2024-05-01' = {
  parent: cosmosPe
  name: 'cosmos-dns-group'
  properties: {
    privateDnsZoneConfigs: [
      { name: 'cosmos-config', properties: { privateDnsZoneId: cosmosZoneId } }
    ]
  }
  dependsOn: [newCosmosLink]
}

// ──── Storage (blob) ─────────────────────────────────────────────────
resource storagePe 'Microsoft.Network/privateEndpoints@2024-05-01' = {
  name: '${storageAccountName}-pe-${suffix}'
  location: location
  properties: {
    subnet: { id: peSubnet.id }
    privateLinkServiceConnections: [
      {
        name: '${storageAccountName}-pls-${suffix}'
        properties: {
          privateLinkServiceId: storageAccountId
          groupIds: ['blob']
        }
      }
    ]
  }
}

resource newBlobZone 'Microsoft.Network/privateDnsZones@2020-06-01' = if (empty(blobZoneRg)) {
  name: blobZoneName
  location: 'global'
}
resource existingBlobZone 'Microsoft.Network/privateDnsZones@2020-06-01' existing = if (!empty(blobZoneRg)) {
  name: blobZoneName
  scope: resourceGroup(resolvedDnsZonesSubscriptionId, blobZoneRg)
}
#disable-next-line BCP318
var blobZoneId = empty(blobZoneRg) ? newBlobZone.id : existingBlobZone.id

resource newBlobLink 'Microsoft.Network/privateDnsZones/virtualNetworkLinks@2024-06-01' = if (empty(blobZoneRg)) {
  parent: newBlobZone
  location: 'global'
  name: 'blob-${suffix}-link'
  properties: {
    virtualNetwork: { id: vnet.id }
    registrationEnabled: false
  }
}

resource storageDnsGroup 'Microsoft.Network/privateEndpoints/privateDnsZoneGroups@2024-05-01' = {
  parent: storagePe
  name: 'blob-dns-group'
  properties: {
    privateDnsZoneConfigs: [
      { name: 'blob-config', properties: { privateDnsZoneId: blobZoneId } }
    ]
  }
  dependsOn: [newBlobLink]
}

// ──── AI Search ──────────────────────────────────────────────────────
resource searchPe 'Microsoft.Network/privateEndpoints@2024-05-01' = {
  name: '${searchServiceName}-pe-${suffix}'
  location: location
  properties: {
    subnet: { id: peSubnet.id }
    privateLinkServiceConnections: [
      {
        name: '${searchServiceName}-pls-${suffix}'
        properties: {
          privateLinkServiceId: searchServiceId
          groupIds: ['searchService']
        }
      }
    ]
  }
}

resource newSearchZone 'Microsoft.Network/privateDnsZones@2020-06-01' = if (empty(searchZoneRg)) {
  name: searchZoneName
  location: 'global'
}
resource existingSearchZone 'Microsoft.Network/privateDnsZones@2020-06-01' existing = if (!empty(searchZoneRg)) {
  name: searchZoneName
  scope: resourceGroup(resolvedDnsZonesSubscriptionId, searchZoneRg)
}
#disable-next-line BCP318
var searchZoneId = empty(searchZoneRg) ? newSearchZone.id : existingSearchZone.id

resource newSearchLink 'Microsoft.Network/privateDnsZones/virtualNetworkLinks@2024-06-01' = if (empty(searchZoneRg)) {
  parent: newSearchZone
  location: 'global'
  name: 'search-${suffix}-link'
  properties: {
    virtualNetwork: { id: vnet.id }
    registrationEnabled: false
  }
}

resource searchDnsGroup 'Microsoft.Network/privateEndpoints/privateDnsZoneGroups@2024-05-01' = {
  parent: searchPe
  name: 'search-dns-group'
  properties: {
    privateDnsZoneConfigs: [
      { name: 'search-config', properties: { privateDnsZoneId: searchZoneId } }
    ]
  }
  dependsOn: [newSearchLink]
}

output cosmosPrivateEndpointId string = cosmosPe.id
output storagePrivateEndpointId string = storagePe.id
output searchPrivateEndpointId string = searchPe.id
