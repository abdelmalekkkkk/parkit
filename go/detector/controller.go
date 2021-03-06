package main

import (
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/jpeg"
	"io"
	"log"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/s3/manager"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	tf "github.com/galeone/tensorflow/tensorflow/go"
	tg "github.com/galeone/tfgo"
	"github.com/gofiber/fiber/v2"
	"gocv.io/x/gocv"
	"gonum.org/v1/gonum/mat"
)

// Controller has handlers for the API requests
type Controller struct {
	UploaderManager *manager.Uploader
	Bucket          string
	Model           *tg.Model
}

type parkingResult struct {
	Occupied uint
	Vacant   uint
}

// UploadImage uploads an image of the parking lot to the S3 bucket
func (controller Controller) UploadImage(c *fiber.Ctx) error {

	errorResponse := c.Status(fiber.StatusBadRequest).JSON("")

	spotsJSON := "[[[80.7766990291262,185.4757281553398],[165.28155339805824,186.71844660194174],[110.60194174757281,253.8252427184466],[24.85436893203883,241.39805825242718]],[[251.0291262135922,190.44660194174756],[169.00970873786406,185.4757281553398],[111.84466019417475,253.8252427184466],[206.29126213592232,256.31067961165047]],[[338.0194174757281,190.44660194174756],[253.5145631067961,189.20388349514562],[208.7766990291262,256.31067961165047],[308.19417475728153,255.06796116504853]],[[423.7669902912621,191.6893203883495],[339.26213592233006,191.6893203883495],[308.19417475728153,255.06796116504853],[398.9126213592233,256.31067961165047]],[[508.2718446601941,195.41747572815532],[417.5533980582524,194.17475728155338],[401.3980582524272,253.8252427184466],[489.631067961165,256.31067961165047]],[[595.26213592233,196.66019417475727],[511.99999999999994,191.6893203883495],[493.3592233009708,256.31067961165047],[585.3203883495145,253.8252427184466]],[[684.7378640776699,195.41747572815532],[595.26213592233,194.17475728155338],[589.0485436893204,256.31067961165047],[679.7669902912621,256.31067961165047]],[[1062.52427184466,358.2135922330097],[1047.6116504854367,407.92233009708735],[1263.8446601941746,456.38834951456306],[1275.0291262135922,399.22330097087377]],[[1025.2427184466019,475.0291262135922],[1046.3689320388348,415.378640776699],[1261.3592233009708,458.87378640776694],[1256.388349514563,533.4368932038834]],[[1001.631067961165,537.1650485436893],[1026.4854368932038,476.2718446601941],[1252.6601941747572,529.7087378640776],[1242.7184466019417,589.3592233009708]],[[1220.3495145631066,672.6213592233009],[969.3203883495145,621.6699029126213],[997.9029126213592,535.9223300970873],[1241.4757281553398,590.6019417475727]],[[925.8252427184465,703.6893203883494],[966.8349514563106,620.4271844660194],[1220.3495145631066,672.6213592233009],[1201.7087378640776,770.7961165048544]],[[874.8737864077669,804.3495145631067],[925.8252427184465,707.4174757281553],[1204.1941747572814,774.5242718446601],[1181.8252427184466,890.0970873786407]],[[821.4368932038834,941.0485436893204],[872.3883495145631,805.5922330097087],[1184.3106796116504,888.8543689320387],[1181.8252427184466,957.2038834951455]]]"

	var spots [][][]float64

	if err := json.Unmarshal([]byte(spotsJSON), &spots); err != nil {
		fmt.Print("Error unmarshling")
	}

	file, err := c.FormFile("image")

	if err != nil {
		return errorResponse
	}

	buffer, err := file.Open()

	if err != nil {
		return errorResponse
	}

	defer buffer.Close()

	decoded, err := jpeg.Decode(buffer)

	if err != nil {
		return errorResponse
	}

	controller.detectSpots(decoded, spots)

	// fileName := strconv.FormatInt(time.Now().Unix(), 10) + ".jpg"

	// go controller.uploadToBucket(fileName, buffer)

	return c.Status(fiber.StatusOK).JSON(fiber.Map{"message": "Image sent successfully"})

}

func (controller Controller) detectSpots(original image.Image, spots [][][]float64) parkingResult {

	var result parkingResult

	img, _ := gocv.ImageToMatRGB(original)

	for _, spot := range spots {

		var points []float64

		for i := 0; i < 4; i++ {
			for j := 0; j < 2; j++ {
				points = append(points, spot[i][j])
			}
		}

		ptsMat := mat.NewDense(4, 2, points)

		transformedImage := FourPointTransform(img, ptsMat)

		final := gocv.NewMat()

		gocv.Resize(transformedImage, &final, image.Point{X: 128, Y: 128}, 0, 0, gocv.InterpolationLinear)

		spotImage, _ := final.ToImage()

		tensor, _ := imageToTensor(spotImage, 128, 128)

		results := controller.Model.Exec([]tf.Output{
			controller.Model.Op("StatefulPartitionedCall", 0),
		}, map[tf.Output]*tf.Tensor{
			controller.Model.Op("serving_default_sequential_1_input", 0): tensor,
		})

		probabilities := results[0].Value().([][]float32)

		if probabilities[0][1] > probabilities[0][0] {
			result.Vacant++
		} else {
			result.Occupied++
		}

	}

	fmt.Printf("%+v", result)

	return result
}

func (controller Controller) uploadToBucket(fileName string, file io.Reader) {
	result, err := controller.UploaderManager.Upload(context.TODO(), &s3.PutObjectInput{
		Bucket: aws.String(controller.Bucket),
		Key:    aws.String(fileName),
		Body:   file,
	})

	if err != nil {
		log.Printf("Error while uploading: %v", err)
		return
	}

	log.Printf("Image uploaded successfully: %+v", result)
}
