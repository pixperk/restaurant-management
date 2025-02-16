package controllers

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"restaurant-management/database"
	"restaurant-management/models"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/go-playground/validator/v10"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

var foodCollection *mongo.Collection = database.OpenCollection(database.Client, "food")
var validate = validator.New()

func GetFoods() gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Second)
		defer cancel()

		// Get recordsPerPage with default value
		recordsPerPage, err := strconv.Atoi(c.Query("recordsPerPage"))
		if err != nil || recordsPerPage < 1 {
			recordsPerPage = 10
		}

		// Get page number with default value
		page, err := strconv.Atoi(c.Query("page"))
		if err != nil || page < 1 {
			page = 1
		}

		// Calculate startIndex
		startIndex := (page - 1) * recordsPerPage

		// If startIndex is provided in the query params, override it
		if queryStartIndex := c.Query("startIndex"); queryStartIndex != "" {
			if parsedStartIndex, err := strconv.Atoi(queryStartIndex); err == nil {
				startIndex = parsedStartIndex
			}
		}

		// MongoDB Aggregation Pipeline
		matchStage := bson.D{{Key: "$match", Value: bson.D{}}}
		groupStage := bson.D{
			{Key: "$group", Value: bson.D{
				{Key: "_id", Value: nil},
				{Key: "total_count", Value: bson.D{{Key: "$sum", Value: 1}}},
				{Key: "data", Value: bson.D{{Key: "$push", Value: "$$ROOT"}}},
			}},
		}
		projectStage := bson.D{
			{Key: "$project", Value: bson.D{
				{Key: "_id", Value: 0},
				{Key: "total_count", Value: 1},
				{Key: "food_items", Value: bson.D{{Key: "$slice", Value: []interface{}{"$data", startIndex, recordsPerPage}}}},
			}},
		}

		// Execute Aggregation
		result, err := foodCollection.Aggregate(ctx, mongo.Pipeline{matchStage, groupStage, projectStage})
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "error occurred while listing food items: " + err.Error()})
			return
		}

		// Decode results
		var allFoods []bson.M
		if err = result.All(ctx, &allFoods); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "error decoding food items: " + err.Error()})
			return
		}

		// Check if there are any results
		if len(allFoods) == 0 {
			c.JSON(http.StatusOK, gin.H{"message": "No food items found"})
			return
		}

		// Return the first result
		c.JSON(http.StatusOK, allFoods[0])
	}
}

func GetFood() gin.HandlerFunc {
	return func(c *gin.Context) {
		// Create a context with a timeout of 100 seconds
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Second)
		defer cancel() // Ensures context is canceled after function execution

		// Extract the "food_id" parameter from the request URL
		foodId := c.Param("food_id")

		//Initialize an empty food struct to store the retrieved data
		var food models.Food

		//Query the MongoDB collection to find the food item by its ID
		err := foodCollection.FindOne(ctx, bson.M{"food_id": foodId}).Decode(&food)
		if err != nil {
			//Handle the error if the food item is not found
			c.JSON(http.StatusNotFound, gin.H{"error": "Food item not found"})
			return
		}

		//Send the retrieved food item as a JSON response
		c.JSON(http.StatusOK, food)
	}
}

func CreateFood() gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Second)
		defer cancel() // Ensures cleanup after function execution

		var food models.Food
		var menu models.Menu

		// Bind JSON request body to food struct
		if err := c.BindJSON(&food); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request data: " + err.Error()})
			return
		}

		// Validate struct fields
		if err := validate.Struct(food); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Validation failed: " + err.Error()})
			return
		}

		// Check if the associated menu exists
		err := menuCollection.FindOne(ctx, bson.M{"menu_id": food.Menu_id}).Decode(&menu)
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "Menu not found"})
			return
		}

		// Assign metadata to the food item
		now := time.Now().Format(time.RFC3339)
		food.Created_at, _ = time.Parse(time.RFC3339, now)
		food.Updated_at, _ = time.Parse(time.RFC3339, now)
		food.ID = primitive.NewObjectID()
		food.Food_id = food.ID.Hex()

		// Ensure price is rounded to 2 decimal places
		if food.Price != nil {
			roundedPrice := toFixed(*food.Price, 2)
			food.Price = &roundedPrice
		}

		// Insert food item into MongoDB
		result, insertErr := foodCollection.InsertOne(ctx, food)
		if insertErr != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Could not create food item"})
			return
		}

		// Return success response
		c.JSON(http.StatusCreated, gin.H{"message": "Food item created", "data": result})
	}
}

func round(num float64) int {
	return int(num + math.Copysign(0.5, num))
}

func toFixed(num float64, precision int) float64 {
	output := math.Pow(10, float64(precision))
	return float64(round(num*output)) / output
}

func UpdateFood() gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Second)
		defer cancel()
		var menu models.Menu
		var food models.Food
		foodId := c.Param("food_id")

		if err := c.BindJSON(&food); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request data: " + err.Error()})
			return
		}

		var updateObj primitive.D
		if food.Name != nil {
			updateObj = append(updateObj, bson.E{Key: "name", Value: food.Name})

		}

		if food.Price != nil {
			updateObj = append(updateObj, bson.E{Key: "price", Value: food.Price})
		}

		if food.Food_image != nil {
			updateObj = append(updateObj, bson.E{Key: "food_image", Value: food.Food_image})
		}

		if food.Menu_id != nil {
			err := menuCollection.FindOne(ctx, bson.M{"menu_id": food.Menu_id}).Decode(&menu)
			if err != nil {
				msg := fmt.Sprintf("message : Menu not found")
				c.JSON(http.StatusInternalServerError, gin.H{"error": msg})
				return
			}
			updateObj = append(updateObj, bson.E{Key: "menu_id", Value: food.Menu_id})

		}

		food.Updated_at, _ = time.Parse(time.RFC3339, time.Now().Format(time.RFC3339))
		updateObj = append(updateObj, bson.E{Key: "updated_at", Value: food.Updated_at})

		upsert := true
		filter := bson.M{"food_id": foodId}

		opt := options.UpdateOptions{Upsert: &upsert}

		result, err := foodCollection.UpdateOne(
			ctx,
			filter,
			bson.D{{Key: "$set", Value: updateObj}},
			&opt,
		)

		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Update failed: " + err.Error()})
			return
		}

		c.JSON(http.StatusOK, gin.H{"message": "Food item updated successfully", "result": result})

	}
}
