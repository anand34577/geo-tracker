package app.geotracker

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test

class ApiTest {
    @Test fun addsMissingScheme() = assertEquals("https://geo.example.com", Api.normalizeServer("geo.example.com"))

    @Test fun keepsHttpAndPort() = assertEquals("http://192.168.1.5:8080", Api.normalizeServer("http://192.168.1.5:8080/"))

    @Test fun repairsSwallowedSlash() {
        assertEquals("https://geo.example.com", Api.normalizeServer("https:/geo.example.com"))
        assertEquals("https://geo.example.com", Api.normalizeServer("https:geo.example.com"))
    }

    @Test fun dropsSpacesAndTrailingSlashes() = assertEquals("https://geo.example.com/app", Api.normalizeServer("  https://geo.example.com/app// "))

    @Test fun rejectsBareScheme() {
        assertNull(Api.normalizeServer("https://"))
        assertNull(Api.normalizeServer("   "))
    }
}
